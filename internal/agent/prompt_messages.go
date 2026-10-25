package agent

import (
	"cmp"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"

	"charm.land/catwalk/pkg/catwalk"
	"charm.land/fantasy"

	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/stringext"
)

// foldedPrompt is a queued prompt taken into a native turn, and where it
// went in that step's messages (before later steps' additions).
type foldedPrompt struct {
	at  int
	msg fantasy.Message
}

// takeQueuedForStep takes the queued prompts that can join call's turn at a
// tool boundary, saves them as user messages and returns them for the step.
func (a *sessionAgent) takeQueuedForStep(ctx context.Context, call SessionAgentCall, supportsImages bool) ([]fantasy.Message, error) {
	mu := a.sessionMu(call.SessionID)
	mu.Lock()
	var fold, canceled []SessionAgentCall
	if queued, _ := a.messageQueue.Get(call.SessionID); len(queued) > 0 && a.compatibleQueuedCalls(call, queued[0]) {
		fold, canceled = a.drainQueueForStepLocked(call.SessionID)
	}
	mu.Unlock()
	a.publishCanceledQueueDrops(canceled)
	if len(fold) == 0 {
		return nil, nil
	}
	if call.onQueuedInput != nil {
		call.onQueuedInput(fold)
	}
	msgs := make([]fantasy.Message, 0, len(fold))
	for _, q := range fold {
		if _, err := a.createUserMessage(ctx, q); err != nil {
			return nil, err
		}
		_, files := a.preparePrompt(nil, supportsImages, q.Attachments...)
		var parts []fantasy.MessagePart
		if text := message.PromptWithTextAttachments(q.Prompt, q.Attachments); text != "" {
			parts = append(parts, fantasy.TextPart{Text: text})
		}
		parts = append(parts, filesAsParts(files)...)
		msgs = append(msgs, fantasy.Message{Role: fantasy.MessageRoleUser, Content: parts})
	}
	return msgs, nil
}

func (a *sessionAgent) createUserMessage(ctx context.Context, call SessionAgentCall) (message.Message, error) {
	if len(call.batch) > 0 {
		var last message.Message
		for _, original := range call.batch {
			msg, err := a.createUserMessage(ctx, original)
			if err != nil {
				return message.Message{}, err
			}
			last = msg
		}
		return last, nil
	}
	parts := []message.ContentPart{message.TextContent{Text: call.Prompt, Hidden: call.HiddenUserMessage, SubmissionID: call.SubmissionID}}
	var attachmentParts []message.ContentPart
	for _, attachment := range call.Attachments {
		attachmentParts = append(attachmentParts, message.BinaryContent{Path: cmp.Or(attachment.FilePath, attachment.FileName), MIMEType: attachment.MimeType, Data: attachment.Content})
	}
	parts = append(parts, attachmentParts...)
	msg, err := a.messages.Create(ctx, call.SessionID, message.CreateMessageParams{
		Role:  message.User,
		Parts: parts,
	})
	if err != nil {
		return message.Message{}, fmt.Errorf("failed to create user message: %w", err)
	}
	return msg, nil
}

func (a *sessionAgent) preparePrompt(msgs []message.Message, supportsImages bool, attachments ...message.Attachment) ([]fantasy.Message, []fantasy.FilePart) {
	var history []fantasy.Message
	if !a.isSubAgent {
		history = append(history, fantasy.NewUserMessage(
			fmt.Sprintf(
				"<system_reminder>%s</system_reminder>",
				`This is a reminder that your todo list is currently empty. DO NOT mention this to the user explicitly because they are already aware.
If you are working on tasks that would benefit from a todo list please use the "todos" tool to create one.
If not, please feel free to ignore. Again do not mention this message to the user.`,
			),
		))
	}
	// Collect all tool call IDs present in assistant messages, then index
	// every tool result by its call ID. Tool results are re-emitted right
	// after the assistant message that requested them instead of at their
	// stored position: messages can be written to a session concurrently
	// (e.g. resuming while a tool is still running), which interleaves
	// user messages between a tool call and its result. LLM APIs require
	// every tool call to be followed by its results before any other
	// message, and strict-adjacency providers (e.g. Kimi, DeepSeek) reject
	// the request otherwise, permanently locking the session.
	knownToolCallIDs := make(map[string]struct{})
	for _, m := range msgs {
		if m.Role != message.Assistant {
			continue
		}
		for _, tc := range m.ToolCalls() {
			knownToolCallIDs[tc.ID] = struct{}{}
		}
	}
	toolResultsByCall := make(map[string][]fantasy.MessagePart)
	for _, m := range msgs {
		if m.Role != message.Tool {
			continue
		}
		for _, aiMsg := range m.ToAIMessage() {
			for _, part := range aiMsg.Content {
				tr, ok := fantasy.AsMessagePart[fantasy.ToolResultPart](part)
				if !ok {
					// Tool-role ToAIMessage only emits ToolResultParts today;
					// log so unexpected parts do not vanish silently.
					slog.Warn(
						"Dropping unexpected non-tool-result part from tool message",
						"part_type", fmt.Sprintf("%T", part),
					)
					continue
				}
				if _, known := knownToolCallIDs[tr.ToolCallID]; !known {
					slog.Warn(
						"Dropping orphaned tool result with no matching tool call",
						"tool_call_id", tr.ToolCallID,
					)
					continue
				}
				toolResultsByCall[tr.ToolCallID] = append(toolResultsByCall[tr.ToolCallID], part)
			}
		}
	}

	for _, m := range msgs {
		if len(m.Parts) == 0 {
			continue
		}
		// Assistant message without content, tool calls, or error (e.g. cancelled before it returned anything).
		if m.Role == message.Assistant && len(m.ToolCalls()) == 0 && m.Content().Text == "" && m.ReasoningContent().String() == "" && !m.IsErrorLike() {
			continue
		}
		// Tool results are emitted right after their assistant message.
		if m.Role == message.Tool {
			continue
		}
		aiMsgs := m.ToAIMessage()
		if !supportsImages {
			for i := range aiMsgs {
				if aiMsgs[i].Role == fantasy.MessageRoleUser {
					aiMsgs[i].Content = filterFileParts(aiMsgs[i].Content)
				}
			}
		}
		history = append(history, aiMsgs...)

		if m.Role == message.Assistant && len(m.ToolCalls()) > 0 {
			history = append(history, toolResultsForCalls(m, toolResultsByCall))
		}
	}

	var files []fantasy.FilePart
	for _, attachment := range attachments {
		if attachment.IsText() {
			continue
		}
		if !supportsImages {
			continue
		}
		files = append(files, fantasy.FilePart{
			Filename:  attachment.FileName,
			Data:      attachment.Content,
			MediaType: attachment.MimeType,
		})
	}

	return history, files
}

func filesAsParts(files []fantasy.FilePart) []fantasy.MessagePart {
	parts := make([]fantasy.MessagePart, 0, len(files))
	for _, f := range files {
		parts = append(parts, f)
	}
	return parts
}

// filterFileParts removes fantasy.FilePart entries from a slice of message
// parts. Used to strip image attachments from historical user messages when
// the current model does not support them.
func filterFileParts(parts []fantasy.MessagePart) []fantasy.MessagePart {
	filtered := make([]fantasy.MessagePart, 0, len(parts))
	for _, part := range parts {
		if _, ok := fantasy.AsMessagePart[fantasy.FilePart](part); ok {
			continue
		}
		filtered = append(filtered, part)
	}
	return filtered
}

// toolResultsForCalls builds the tool message that must immediately follow
// an assistant message with tool calls. LLM APIs require every tool call to
// be followed by its results before any other message; strict-adjacency
// providers reject the request otherwise. Results are taken from
// toolResultsByCall and consumed, so a result stored in a message that also
// holds results for calls of other assistant messages is emitted exactly
// once, next to the assistant that requested it. Tool calls without any
// stored result (e.g. an interrupted session) receive a synthetic error
// response so the conversation keeps working.
func toolResultsForCalls(m message.Message, toolResultsByCall map[string][]fantasy.MessagePart) fantasy.Message {
	content := make([]fantasy.MessagePart, 0, len(m.ToolCalls()))
	for _, tc := range m.ToolCalls() {
		parts := toolResultsByCall[tc.ID]
		delete(toolResultsByCall, tc.ID)
		if len(parts) > 0 {
			content = append(content, parts...)
			continue
		}
		slog.Warn(
			"Injecting synthetic tool result for orphaned tool call",
			"tool_call_id", tc.ID,
			"tool_name", tc.Name,
		)
		content = append(content, fantasy.ToolResultPart{
			ToolCallID: tc.ID,
			Output: fantasy.ToolResultOutputContentError{
				Error: errors.New("tool call was interrupted and did not produce a result, you may retry this call if the result is still needed"),
			},
		})
	}
	return fantasy.Message{
		Role:    fantasy.MessageRoleTool,
		Content: content,
	}
}

func (a *sessionAgent) getSessionMessages(ctx context.Context, session session.Session) ([]message.Message, error) {
	// Read only the tail a compacted session actually sends. The full
	// transcript can be tens of megabytes on the single shared connection.
	msgs, err := a.messages.ListFromSummary(ctx, session.ID, session.SummaryMessageID)
	if err != nil {
		return nil, fmt.Errorf("failed to list messages: %w", err)
	}

	if session.SummaryMessageID != "" {
		summaryMsgIndex := -1
		for i, msg := range msgs {
			if msg.ID == session.SummaryMessageID {
				summaryMsgIndex = i
				break
			}
		}
		if summaryMsgIndex != -1 {
			msgs = msgs[summaryMsgIndex:]
			msgs[0].Role = message.User
		}
	}
	return msgs, nil
}

// hasUserTextMessage reports whether any user message in msgs contains
// text content (as opposed to only shell commands or other non-text parts).
func hasUserTextMessage(msgs []message.Message) bool {
	for _, msg := range msgs {
		if msg.Role != message.User {
			continue
		}
		for _, part := range msg.Parts {
			if tc, ok := part.(message.TextContent); ok && tc.Text != "" {
				return true
			}
		}
	}
	return false
}

// convertToToolResult converts a fantasy tool result to a message tool result.
func (a *sessionAgent) convertToToolResult(result fantasy.ToolResultContent) message.ToolResult {
	baseResult := message.ToolResult{
		ToolCallID: result.ToolCallID,
		Name:       result.ToolName,
		Metadata:   result.ClientMetadata,
	}

	switch result.Result.GetType() {
	case fantasy.ToolResultContentTypeText:
		if r, ok := fantasy.AsToolResultOutputType[fantasy.ToolResultOutputContentText](result.Result); ok {
			baseResult.Content = r.Text
		}
	case fantasy.ToolResultContentTypeError:
		if r, ok := fantasy.AsToolResultOutputType[fantasy.ToolResultOutputContentError](result.Result); ok {
			baseResult.Content = r.Error.Error()
			baseResult.IsError = true
		}
	case fantasy.ToolResultContentTypeMedia:
		if r, ok := fantasy.AsToolResultOutputType[fantasy.ToolResultOutputContentMedia](result.Result); ok {
			if !stringext.IsValidBase64(r.Data) {
				slog.Warn(
					"Tool returned media with invalid base64 data, discarding image",
					"tool", result.ToolName,
					"tool_call_id", result.ToolCallID,
				)
				baseResult.Content = "Tool returned image data with invalid encoding"
				baseResult.IsError = true
			} else {
				content := r.Text
				if content == "" {
					content = fmt.Sprintf("Loaded %s content", r.MediaType)
				}
				baseResult.Content = content
				baseResult.Data = r.Data
				baseResult.MIMEType = r.MediaType
			}
		}
	}

	return baseResult
}

// workaroundProviderMediaLimitations converts media content in tool results to
// user messages for providers that don't natively support images in tool results.
//
// Problem: OpenAI, Google, OpenRouter, and other OpenAI-compatible providers
// don't support sending images/media in tool result messages - they only accept
// text in tool results. However, they DO support images in user messages.
//
// If we send media in tool results to these providers, the API returns an error.
//
// Solution: For these providers, we:
//  1. Replace the media in the tool result with a text placeholder
//  2. Inject a user message immediately after with the image as a file attachment
//  3. This maintains the tool execution flow while working around API limitations
//
// Anthropic and Bedrock support images natively in tool results, so we skip
// this workaround for them.
//
// Example transformation:
//
//	BEFORE: [tool result: image data]
//	AFTER:  [tool result: "Image loaded - see attached"], [user: image attachment]
func (a *sessionAgent) workaroundProviderMediaLimitations(messages []fantasy.Message, chatModel Model) []fantasy.Message {
	providerSupportsMedia := chatModel.ModelCfg.Provider == string(catwalk.InferenceProviderAnthropic) ||
		chatModel.ModelCfg.Provider == string(catwalk.InferenceProviderBedrock) ||
		chatModel.ModelCfg.Provider == string(catwalk.InferenceProviderBedrockEurope)

	if providerSupportsMedia {
		return messages
	}

	supportsImages := chatModel.CatwalkCfg.SupportsImages

	convertedMessages := make([]fantasy.Message, 0, len(messages))

	for _, msg := range messages {
		if msg.Role != fantasy.MessageRoleTool {
			convertedMessages = append(convertedMessages, msg)
			continue
		}

		textParts := make([]fantasy.MessagePart, 0, len(msg.Content))
		var mediaFiles []fantasy.FilePart

		for _, part := range msg.Content {
			toolResult, ok := fantasy.AsMessagePart[fantasy.ToolResultPart](part)
			if !ok {
				textParts = append(textParts, part)
				continue
			}

			if media, ok := fantasy.AsToolResultOutputType[fantasy.ToolResultOutputContentMedia](toolResult.Output); ok {
				if !supportsImages {
					// Model cannot process images. Replace with a text
					// placeholder and skip creating a synthetic user
					// message with FilePart, which would brick the
					// session on text-only models.
					textParts = append(textParts, fantasy.ToolResultPart{
						ToolCallID: toolResult.ToolCallID,
						Output: fantasy.ToolResultOutputContentText{
							Text: "[Image/media content not supported by this model]",
						},
						ProviderOptions: toolResult.ProviderOptions,
					})
					continue
				}

				decoded, err := base64.StdEncoding.DecodeString(media.Data)
				if err != nil {
					slog.Warn("Failed to decode media data", "error", err)
					textParts = append(textParts, part)
					continue
				}

				mediaFiles = append(mediaFiles, fantasy.FilePart{
					Data:      decoded,
					MediaType: media.MediaType,
					Filename:  fmt.Sprintf("tool-result-%s", toolResult.ToolCallID),
				})

				textParts = append(textParts, fantasy.ToolResultPart{
					ToolCallID: toolResult.ToolCallID,
					Output: fantasy.ToolResultOutputContentText{
						Text: "[Image/media content loaded - see attached file]",
					},
					ProviderOptions: toolResult.ProviderOptions,
				})
			} else {
				textParts = append(textParts, part)
			}
		}

		convertedMessages = append(convertedMessages, fantasy.Message{
			Role:    fantasy.MessageRoleTool,
			Content: textParts,
		})

		if len(mediaFiles) > 0 {
			convertedMessages = append(convertedMessages, fantasy.NewUserMessage(
				"Here is the media content from the tool result:",
				mediaFiles...,
			))
		}
	}

	return convertedMessages
}

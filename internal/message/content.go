package message

import (
	"encoding/base64"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"charm.land/catwalk/pkg/catwalk"
	"charm.land/fantasy"
	"charm.land/fantasy/providers/anthropic"
	"charm.land/fantasy/providers/google"
	"charm.land/fantasy/providers/openai"
	"github.com/charmbracelet/crush/internal/filechange"
	"github.com/charmbracelet/crush/internal/stringext"
	"github.com/charmbracelet/x/ansi"
)

type MessageRole string

const (
	Assistant MessageRole = "assistant"
	User      MessageRole = "user"
	System    MessageRole = "system"
	Tool      MessageRole = "tool"
)

// mediaLoadFailedPlaceholder is the text substituted for image data that
// cannot be decoded during session replay.
const mediaLoadFailedPlaceholder = "[Image data could not be loaded]"

type FinishReason string

const (
	FinishReasonEndTurn FinishReason = "end_turn"
	// Branch summaries are notes, not timed replies to the last user prompt.
	FinishReasonBranchSummary FinishReason = "branch_summary"
	FinishReasonMaxTokens     FinishReason = "max_tokens"
	FinishReasonToolUse       FinishReason = "tool_use"
	FinishReasonCanceled      FinishReason = "canceled"
	FinishReasonError         FinishReason = "error"
	// FinishReasonContentFilter is a provider safety/refusal stop
	// (Anthropic stop_reason=refusal, OpenAI content_filter, etc.).
	// The TUI renders this as a REFUSED banner rather than a silent
	// empty turn.
	FinishReasonContentFilter FinishReason = "content_filter"

	// Should never happen
	FinishReasonUnknown FinishReason = "unknown"
)

type ContentPart interface {
	isPart()
}

type ReasoningContent struct {
	Thinking         string                             `json:"thinking"`
	Signature        string                             `json:"signature"`
	ThoughtSignature string                             `json:"thought_signature"` // Used for google
	ToolID           string                             `json:"tool_id"`           // Used for openrouter google models
	ResponsesData    *openai.ResponsesReasoningMetadata `json:"responses_data"`
	StartedAt        int64                              `json:"started_at,omitempty"`
	FinishedAt       int64                              `json:"finished_at,omitempty"`
	RedactedData     string                             `json:"redacted_data,omitempty"`
	Placed           bool                               `json:"placed,omitempty"`
	TextOffset       int                                `json:"text_offset,omitempty"`
	ToolCallIndex    int                                `json:"tool_call_index,omitempty"`
}

func (tc ReasoningContent) String() string {
	return tc.Thinking
}
func (ReasoningContent) isPart() {}

type TextContent struct {
	Text         string `json:"text"`
	SubmissionID string `json:"submission_id,omitempty"`
	// Hidden marks generated user continuations that remain in model history.
	Hidden bool `json:"hidden,omitempty"`
}

func (tc TextContent) String() string {
	return tc.Text
}

func (TextContent) isPart() {}

type ImageURLContent struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"`
}

func (iuc ImageURLContent) String() string {
	return iuc.URL
}

func (ImageURLContent) isPart() {}

type BinaryContent struct {
	Path     string
	MIMEType string
	Data     []byte
}

func (bc BinaryContent) String(p catwalk.InferenceProvider) string {
	base64Encoded := base64.StdEncoding.EncodeToString(bc.Data)
	if p == catwalk.InferenceProviderOpenAI {
		return "data:" + bc.MIMEType + ";base64," + base64Encoded
	}
	return base64Encoded
}

func (BinaryContent) isPart() {}

type ToolCall struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Input            string `json:"input"`
	ProviderExecuted bool   `json:"provider_executed"`
	Finished         bool   `json:"finished"`
}

func (ToolCall) isPart() {}

type ToolResult struct {
	ToolCallID string             `json:"tool_call_id"`
	Name       string             `json:"name"`
	Content    string             `json:"content"`
	Data       string             `json:"data"`
	MIMEType   string             `json:"mime_type"`
	Metadata   string             `json:"metadata"`
	IsError    bool               `json:"is_error"`
	Review     *filechange.Review `json:"review,omitempty"`
}

func (ToolResult) isPart() {}

type Finish struct {
	Reason  FinishReason `json:"reason"`
	Time    int64        `json:"time"`
	Message string       `json:"message,omitempty"`
	Details string       `json:"details,omitempty"`
}

func (Finish) isPart() {}

// ShellCommand stores a bang-mode shell command and its output as a
// distinct content part so it can be reconstructed on session restore.
type ShellCommand struct {
	Command  string `json:"command"`
	Output   string `json:"output"`
	ExitCode int    `json:"exit_code"`
}

func (ShellCommand) isPart() {}

// HasShellCommand reports whether the message contains any ShellCommand parts.
func (m *Message) HasShellCommand() bool {
	for _, part := range m.Parts {
		if _, ok := part.(ShellCommand); ok {
			return true
		}
	}
	return false
}

// ShellCommands returns all ShellCommand parts from the message.
func (m *Message) ShellCommands() []ShellCommand {
	var cmds []ShellCommand
	for _, part := range m.Parts {
		if sc, ok := part.(ShellCommand); ok {
			cmds = append(cmds, sc)
		}
	}
	return cmds
}

type Message struct {
	ID               string
	Role             MessageRole
	SessionID        string
	Parts            []ContentPart
	Model            string
	Provider         string
	CreatedAt        int64
	UpdatedAt        int64
	IsSummaryMessage bool
	// IsCompacting is live CLI status; it is not persisted in history.
	IsCompacting bool
	// ActivityAt records the last live provider event, not a timer tick.
	ActivityAt int64
	Activity   string
	// ActivityDetail is the newest line of thinking a CLI holds back
	// until the thought ends, so it isn't on screen yet.
	ActivityDetail string
	// PrismModelID and PrismModelName identify the model that actually
	// served the turn, as reported by the Hyper Prism model router
	// headers. Empty when the turn was not routed through Prism.
	PrismModelID   string
	PrismModelName string
	// PrismHypercreditSavings and PrismDollarSavings are the savings
	// from routing through Prism, as reported by its savings trailers.
	// Nil when not reported. When both are present the hypercredit
	// figure is the one shown.
	PrismHypercreditSavings *float64
	PrismDollarSavings      *float64
}

func (m *Message) Content() TextContent {
	for _, part := range m.Parts {
		if c, ok := part.(TextContent); ok {
			return c
		}
	}
	return TextContent{}
}

func (m *Message) ReasoningContent() ReasoningContent {
	blocks := m.ReasoningBlocks()
	if len(blocks) == 0 {
		return ReasoningContent{}
	}
	combined := blocks[0]
	for _, block := range blocks[1:] {
		if block.Thinking != "" {
			if combined.Thinking != "" {
				combined.Thinking += "\n\n"
			}
			combined.Thinking += block.Thinking
		}
		if block.FinishedAt == 0 {
			combined.FinishedAt = 0
		} else if combined.FinishedAt != 0 {
			combined.FinishedAt = block.FinishedAt
		}
	}
	return combined
}

func (m *Message) ReasoningBlocks() []ReasoningContent {
	var blocks []ReasoningContent
	for _, part := range m.Parts {
		if c, ok := part.(ReasoningContent); ok {
			blocks = append(blocks, c)
		}
	}
	return blocks
}

func (m *Message) lastReasoningIndex() int {
	for i := len(m.Parts) - 1; i >= 0; i-- {
		if _, ok := m.Parts[i].(ReasoningContent); ok {
			return i
		}
	}
	return -1
}

func (m *Message) ImageURLContent() []ImageURLContent {
	imageURLContents := make([]ImageURLContent, 0)
	for _, part := range m.Parts {
		if c, ok := part.(ImageURLContent); ok {
			imageURLContents = append(imageURLContents, c)
		}
	}
	return imageURLContents
}

func (m *Message) BinaryContent() []BinaryContent {
	binaryContents := make([]BinaryContent, 0)
	for _, part := range m.Parts {
		if c, ok := part.(BinaryContent); ok {
			binaryContents = append(binaryContents, c)
		}
	}
	return binaryContents
}

func (m *Message) ToolCalls() []ToolCall {
	toolCalls := make([]ToolCall, 0)
	for _, part := range m.Parts {
		if c, ok := part.(ToolCall); ok {
			toolCalls = append(toolCalls, c)
		}
	}
	return toolCalls
}

func (m *Message) ToolResults() []ToolResult {
	toolResults := make([]ToolResult, 0)
	for _, part := range m.Parts {
		if c, ok := part.(ToolResult); ok {
			toolResults = append(toolResults, c)
		}
	}
	return toolResults
}

func (m *Message) IsFinished() bool {
	for _, part := range m.Parts {
		if _, ok := part.(Finish); ok {
			return true
		}
	}
	return false
}

func (m *Message) FinishPart() *Finish {
	for _, part := range m.Parts {
		if c, ok := part.(Finish); ok {
			return &c
		}
	}
	return nil
}

func (m *Message) FinishReason() FinishReason {
	for _, part := range m.Parts {
		if c, ok := part.(Finish); ok {
			return c.Reason
		}
	}
	return ""
}

// IsErrorLike reports whether the message finished with an error-style
// banner (a real error or a provider safety refusal). The TUI renders
// both through the same banner path.
func (m *Message) IsErrorLike() bool {
	switch m.FinishReason() {
	case FinishReasonError, FinishReasonContentFilter:
		return true
	}
	return false
}

// ErrorText returns a formatted error banner string if the message finished
// with an error or content filter refusal, or empty string otherwise.
func (m *Message) ErrorText() string {
	if !m.IsErrorLike() {
		return ""
	}
	f := m.FinishPart()
	if f == nil {
		return ""
	}
	title := strings.TrimSpace(f.Message)
	if title == "" {
		title = "Error"
	}
	details := strings.TrimSpace(f.Details)
	if details != "" {
		return fmt.Sprintf("[error: %s]\n%s", title, details)
	}
	return fmt.Sprintf("[error: %s]", title)
}

func (m *Message) IsThinking() bool {
	reasoning := m.ReasoningContent()
	return (reasoning.StartedAt > 0 || reasoning.Thinking != "") && reasoning.FinishedAt == 0 && m.Content().Text == "" && !m.IsFinished()
}

func (m *Message) AppendContent(delta string) {
	found := false
	for i, part := range m.Parts {
		if c, ok := part.(TextContent); ok {
			c.Text += delta
			m.Parts[i] = c
			found = true
		}
	}
	if !found {
		m.Parts = append(m.Parts, TextContent{Text: delta})
	}
}

func (m *Message) StartReasoning(text string) {
	m.Parts = append(m.Parts, ReasoningContent{
		Thinking:      text,
		StartedAt:     time.Now().Unix(),
		Placed:        true,
		TextOffset:    len(m.Content().Text),
		ToolCallIndex: len(m.ToolCalls()),
	})
}

func (m *Message) openReasoningIndex() int {
	i := m.lastReasoningIndex()
	if i == -1 || m.Parts[i].(ReasoningContent).FinishedAt != 0 {
		m.StartReasoning("")
		return len(m.Parts) - 1
	}
	return i
}

func (m *Message) AppendReasoningContent(delta string) {
	i := m.openReasoningIndex()
	c := m.Parts[i].(ReasoningContent)
	c.Thinking += delta
	m.Parts[i] = c
}

func (m *Message) AppendThoughtSignature(signature string, toolCallID string) {
	i := m.openReasoningIndex()
	c := m.Parts[i].(ReasoningContent)
	c.ThoughtSignature += signature
	c.ToolID = toolCallID
	m.Parts[i] = c
}

func (m *Message) AppendReasoningSignature(signature string) {
	i := m.openReasoningIndex()
	c := m.Parts[i].(ReasoningContent)
	c.Signature += signature
	m.Parts[i] = c
}

func (m *Message) SetReasoningRedactedData(data string) {
	i := m.openReasoningIndex()
	c := m.Parts[i].(ReasoningContent)
	c.RedactedData = data
	m.Parts[i] = c
}

func (m *Message) SetReasoningResponsesData(data *openai.ResponsesReasoningMetadata) {
	i := m.lastReasoningIndex()
	if i == -1 {
		return
	}
	c := m.Parts[i].(ReasoningContent)
	c.ResponsesData = data
	m.Parts[i] = c
}

func (m *Message) FinishThinking() {
	now := time.Now().Unix()
	for i, part := range m.Parts {
		if c, ok := part.(ReasoningContent); ok && c.FinishedAt == 0 {
			c.FinishedAt = now
			m.Parts[i] = c
		}
	}
}

func (m *Message) ThinkingDuration() time.Duration {
	reasoning := m.ReasoningContent()
	if reasoning.StartedAt == 0 {
		return 0
	}

	endTime := reasoning.FinishedAt
	if endTime == 0 {
		endTime = time.Now().Unix()
	}

	return time.Duration(endTime-reasoning.StartedAt) * time.Second
}

func (m *Message) FinishToolCall(toolCallID string) {
	for i, part := range m.Parts {
		if c, ok := part.(ToolCall); ok {
			if c.ID == toolCallID {
				m.Parts[i] = ToolCall{
					ID:       c.ID,
					Name:     c.Name,
					Input:    c.Input,
					Finished: true,
				}
				return
			}
		}
	}
}

func (m *Message) AppendToolCallInput(toolCallID string, inputDelta string) {
	for i, part := range m.Parts {
		if c, ok := part.(ToolCall); ok {
			if c.ID == toolCallID {
				m.Parts[i] = ToolCall{
					ID:       c.ID,
					Name:     c.Name,
					Input:    c.Input + inputDelta,
					Finished: c.Finished,
				}
				return
			}
		}
	}
}

func (m *Message) AddToolCall(tc ToolCall) {
	for i, part := range m.Parts {
		if c, ok := part.(ToolCall); ok {
			if c.ID == tc.ID {
				m.Parts[i] = tc
				return
			}
		}
	}
	m.Parts = append(m.Parts, tc)
}

func (m *Message) SetToolCalls(tc []ToolCall) {
	// remove any existing tool call part it could have multiple
	parts := make([]ContentPart, 0)
	for _, part := range m.Parts {
		if _, ok := part.(ToolCall); ok {
			continue
		}
		parts = append(parts, part)
	}
	m.Parts = parts
	for _, toolCall := range tc {
		m.Parts = append(m.Parts, toolCall)
	}
}

func (m *Message) AddToolResult(tr ToolResult) {
	m.Parts = append(m.Parts, tr)
}

func (m *Message) SetToolResults(tr []ToolResult) {
	for _, toolResult := range tr {
		m.Parts = append(m.Parts, toolResult)
	}
}

// Clone returns a deep copy of the message with an independent Parts slice.
// This prevents race conditions when the message is modified concurrently.
func (m *Message) Clone() Message {
	clone := *m
	clone.Parts = make([]ContentPart, len(m.Parts))
	copy(clone.Parts, m.Parts)
	return clone
}

// ResetStreamedContent removes all parts that were added during streaming
// (text, reasoning, tool calls, finish) so the message is ready for a
// retry. Non-streamed parts (images, binary attachments, tool results,
// shell commands) are preserved.
func (m *Message) ResetStreamedContent() {
	kept := m.Parts[:0]
	for _, part := range m.Parts {
		switch part.(type) {
		case TextContent, ReasoningContent, ToolCall, Finish:
			// Drop streamed parts.
		default:
			kept = append(kept, part)
		}
	}
	m.Parts = kept
}

func (m *Message) AddFinish(reason FinishReason, message, details string) {
	m.IsCompacting = false
	// remove any existing finish part
	for i, part := range m.Parts {
		if _, ok := part.(Finish); ok {
			m.Parts = slices.Delete(m.Parts, i, i+1)
			break
		}
	}
	m.Parts = append(m.Parts, Finish{Reason: reason, Time: time.Now().Unix(), Message: message, Details: details})
}

func (m *Message) AddImageURL(url, detail string) {
	m.Parts = append(m.Parts, ImageURLContent{URL: url, Detail: detail})
}

func (m *Message) AddBinary(mimeType string, data []byte) {
	m.Parts = append(m.Parts, BinaryContent{MIMEType: mimeType, Data: data})
}

func PromptWithTextAttachments(prompt string, attachments []Attachment) string {
	var sb strings.Builder
	sb.WriteString(prompt)
	addedAttachments := false
	for _, content := range attachments {
		if !content.IsText() {
			continue
		}
		if !addedAttachments {
			sb.WriteString("\n<system_info>The files below have been attached by the user, consider them in your response</system_info>\n")
			addedAttachments = true
		}
		if content.FilePath != "" {
			fmt.Fprintf(&sb, "<file path='%s'>\n", content.FilePath)
		} else {
			sb.WriteString("<file>\n")
		}
		sb.WriteString("\n")
		sb.Write(content.Content)
		sb.WriteString("\n</file>\n")
	}
	return sb.String()
}

func (m *Message) assistantParts() []fantasy.MessagePart {
	text := m.Content().Text
	calls := m.ToolCalls()
	blocks := m.ReasoningBlocks()
	placed := len(blocks) > 0
	for _, block := range blocks {
		placed = placed && block.Placed
	}

	var parts []fantasy.MessagePart
	textAt, callAt := 0, 0
	emit := func(textEnd, callEnd int) {
		textEnd = min(max(textEnd, textAt), len(text))
		callEnd = min(max(callEnd, callAt), len(calls))
		if segment := strings.TrimSpace(text[textAt:textEnd]); segment != "" {
			parts = append(parts, fantasy.TextPart{Text: segment})
		}
		for _, call := range calls[callAt:callEnd] {
			parts = append(parts, fantasy.ToolCallPart{
				ToolCallID:       call.ID,
				ToolName:         call.Name,
				Input:            call.Input,
				ProviderExecuted: call.ProviderExecuted,
			})
		}
		textAt, callAt = textEnd, callEnd
	}

	if !placed {
		emit(len(text), 0)
		for _, block := range blocks {
			if part, ok := reasoningPart(block, false); ok {
				parts = append(parts, part)
			}
		}
		emit(len(text), len(calls))
		if errText := m.ErrorText(); errText != "" {
			parts = append(parts, fantasy.TextPart{Text: errText})
		}
		return parts
	}

	for _, block := range blocks {
		emit(block.TextOffset, block.ToolCallIndex)
		if part, ok := reasoningPart(block, true); ok {
			parts = append(parts, part)
		}
	}
	emit(len(text), len(calls))
	if errText := m.ErrorText(); errText != "" {
		parts = append(parts, fantasy.TextPart{Text: errText})
	}
	return parts
}

func reasoningPart(block ReasoningContent, withAnthropic bool) (fantasy.ReasoningPart, bool) {
	redacted := withAnthropic && block.RedactedData != ""
	if block.Thinking == "" && !redacted {
		return fantasy.ReasoningPart{}, false
	}
	part := fantasy.ReasoningPart{Text: block.Thinking, ProviderOptions: fantasy.ProviderOptions{}}
	if withAnthropic && block.Signature != "" {
		part.ProviderOptions[anthropic.Name] = &anthropic.ReasoningOptionMetadata{Signature: block.Signature}
	} else if redacted {
		part.ProviderOptions[anthropic.Name] = &anthropic.ReasoningOptionMetadata{RedactedData: block.RedactedData}
	}
	if block.ResponsesData != nil {
		part.ProviderOptions[openai.Name] = block.ResponsesData
	}
	if block.ThoughtSignature != "" {
		part.ProviderOptions[google.Name] = &google.ReasoningMetadata{
			Signature: block.ThoughtSignature,
			ToolID:    block.ToolID,
		}
	}
	return part, true
}

func (m *Message) ToAIMessage() []fantasy.Message {
	var messages []fantasy.Message
	switch m.Role {
	case User:
		var parts []fantasy.MessagePart
		text := strings.TrimSpace(m.Content().Text)
		var textAttachments []Attachment
		for _, content := range m.BinaryContent() {
			if !strings.HasPrefix(content.MIMEType, "text/") {
				continue
			}
			textAttachments = append(textAttachments, Attachment{
				FilePath: content.Path,
				MimeType: content.MIMEType,
				Content:  content.Data,
			})
		}
		text = PromptWithTextAttachments(text, textAttachments)
		// Include bang-mode shell commands as context for the agent.
		for _, sc := range m.ShellCommands() {
			shellText := fmt.Sprintf("$ %s\n%s\n(exit code %d)", sc.Command, ansi.Strip(sc.Output), sc.ExitCode)
			if text != "" {
				text += "\n\n" + shellText
			} else {
				text = shellText
			}
		}
		if text != "" {
			parts = append(parts, fantasy.TextPart{Text: text})
		}
		for _, content := range m.BinaryContent() {
			// skip text attachements
			if strings.HasPrefix(content.MIMEType, "text/") {
				continue
			}
			parts = append(parts, fantasy.FilePart{
				Filename:  content.Path,
				Data:      content.Data,
				MediaType: content.MIMEType,
			})
		}
		messages = append(messages, fantasy.Message{
			Role:    fantasy.MessageRoleUser,
			Content: parts,
		})
	case Assistant:
		messages = append(messages, fantasy.Message{
			Role:    fantasy.MessageRoleAssistant,
			Content: m.assistantParts(),
		})
	case Tool:
		var parts []fantasy.MessagePart
		for _, result := range m.ToolResults() {
			var content fantasy.ToolResultOutputContent
			if result.IsError {
				content = fantasy.ToolResultOutputContentError{
					Error: errors.New(result.Content),
				}
			} else if result.Data != "" {
				if stringext.IsValidBase64(result.Data) {
					content = fantasy.ToolResultOutputContentMedia{
						Data:      result.Data,
						MediaType: result.MIMEType,
					}
				} else {
					content = fantasy.ToolResultOutputContentText{
						Text: mediaLoadFailedPlaceholder,
					}
				}
			} else {
				content = fantasy.ToolResultOutputContentText{
					Text: result.Content,
				}
			}
			parts = append(parts, fantasy.ToolResultPart{
				ToolCallID: result.ToolCallID,
				Output:     content,
			})
		}
		messages = append(messages, fantasy.Message{
			Role:    fantasy.MessageRoleTool,
			Content: parts,
		})
	}
	return messages
}

package agent

import (
	"context"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"charm.land/catwalk/v3/pkg/catwalk"
	"charm.land/fantasy"
	"charm.land/fantasy/providers/anthropic"
	"charm.land/fantasy/providers/google"
	"charm.land/fantasy/providers/openai"

	"github.com/charmbracelet/crush/internal/agent/cliagent"
	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/agent/tools/shelltool"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/session"
)

// onCompacting marks the assistant message while an agent CLI compacts.
func (t *turn) onCompacting(active bool) error {
	if t.currentAssistant == nil {
		return nil
	}
	t.currentAssistant.IsCompacting = active
	return t.a.messages.Update(t.genCtx, *t.currentAssistant)
}

// onActivity records that an agent CLI is still working and, when the CLI
// says, what it is doing and the newest line of thinking it holds back.
func (t *turn) onActivity(activity, detail string) error {
	if t.currentAssistant == nil {
		return nil
	}
	if activity != "" {
		t.currentAssistant.Activity = activity
		t.currentAssistant.ActivityDetail = detail
	}
	t.currentAssistant.ActivityAt = time.Now().Unix()
	return t.a.messages.Update(t.genCtx, *t.currentAssistant)
}

// prepareStep builds each step's messages and creates the step's
// assistant message.
func (t *turn) prepareStep(callContext context.Context, options fantasy.PrepareStepFunctionOptions) (_ context.Context, prepared fantasy.PrepareStepResult, err error) {
	a := t.a
	call := t.call
	chatModel := t.chatModel
	prepared.Messages = options.Messages
	for i := range prepared.Messages {
		prepared.Messages[i].ProviderOptions = nil
	}

	// Use latest tools (updated by SetTools when MCP tools
	// change), filtered for the call and minus MCP servers
	// disabled for this repository.
	prepared.Tools = a.filterDisabledMCPTools(callContext, a.callTools(call))
	if !t.isCLI {
		for i, tool := range prepared.Tools {
			prepared.Tools[i] = progressInterruptTool{AgentTool: tool, turn: t}
		}
	}

	// Prompts queued during the turn join it at the next tool
	// boundary: every step after the first follows tool results.
	// Fantasy rebuilds each step's messages, so earlier folds are
	// put back where they went in.
	if !t.isCLI && options.StepNumber > 0 {
		queued, err := a.takeQueuedForStep(callContext, call, chatModel.CatwalkCfg.Capabilities.Vision, chatModel.CatwalkCfg.Capabilities.Audio)
		if err != nil {
			return callContext, prepared, err
		}
		for _, msg := range queued {
			t.folded = append(t.folded, foldedPrompt{at: len(options.Messages), msg: msg})
		}
	}
	for i, f := range t.folded {
		prepared.Messages = slices.Insert(prepared.Messages, min(f.at+i, len(prepared.Messages)), f.msg)
	}

	prepared.Messages = dropEmptyToolMedia(prepared.Messages)
	prepared.Messages = a.workaroundProviderMediaLimitations(prepared.Messages, chatModel)

	lastSystemRoleInx := 0
	systemMessageUpdated := false
	for i, msg := range prepared.Messages {
		// Only add cache control to the last message.
		if msg.Role == fantasy.MessageRoleSystem {
			lastSystemRoleInx = i
		} else if !systemMessageUpdated {
			prepared.Messages[lastSystemRoleInx].ProviderOptions = a.getCacheControlOptions()
			systemMessageUpdated = true
		}
		// Than add cache control to the last 2 messages.
		if i > len(prepared.Messages)-3 {
			prepared.Messages[i].ProviderOptions = a.getCacheControlOptions()
		}
	}

	if t.promptPrefix != "" {
		prepared.Messages = append([]fantasy.Message{fantasy.NewSystemMessage(t.promptPrefix)}, prepared.Messages...)
	}

	t.sessionLock.Lock()
	t.stepMessages = cloneFantasyMessages(prepared.Messages)
	t.sessionLock.Unlock()

	var assistantMsg message.Message
	assistantMsg, err = a.messages.Create(callContext, call.SessionID, message.CreateMessageParams{
		Role:     message.Assistant,
		Parts:    []message.ContentPart{},
		Model:    chatModel.ModelCfg.Model,
		Provider: chatModel.ModelCfg.Provider,
	})
	if err != nil {
		return callContext, prepared, err
	}
	callContext = context.WithValue(callContext, tools.MessageIDContextKey, assistantMsg.ID)
	callContext = context.WithValue(callContext, tools.ChannelContextKey, call.Channel)
	callContext = context.WithValue(callContext, tools.SupportsImagesContextKey, chatModel.CatwalkCfg.Capabilities.Vision)
	callContext = context.WithValue(callContext, tools.ModelNameContextKey, chatModel.CatwalkCfg.Name)
	callContext = context.WithValue(callContext, tools.ChatModelContextKey, chatModel.ModelCfg)
	t.currentAssistant = &assistantMsg
	return callContext, prepared, err
}

// progressInterruptTool lets the turn cancel the command at its configured
// consecutive tool-call limit before that call can continue running.
type progressInterruptTool struct {
	fantasy.AgentTool
	turn *turn
}

func (p progressInterruptTool) Run(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	if p.turn.interruptOnToolCall() {
		return fantasy.ToolResponse{}, context.Canceled
	}
	return p.AgentTool.Run(ctx, call)
}

func (t *turn) progressUpdatesEnabled() bool {
	return !t.isCLI && config.AnthropicProgressUpdates(t.chatModel.ModelCfg.Provider, t.chatModel.ModelCfg.Model)
}

func (t *turn) onReasoningStart(id string, reasoning fantasy.ReasoningContent) error {
	t.currentAssistant.ActivityDetail = ""
	t.currentAssistant.Activity = "thinking"
	t.currentAssistant.ActivityAt = time.Now().Unix()
	t.currentAssistant.FinishThinking()
	t.currentAssistant.StartReasoning(reasoning.Text)
	if t.progressUpdatesEnabled() && reasoning.Text != "" {
		t.currentAssistant.ShowProgressUpdate(reasoning.Text)
		t.noteVisibleAssistantText()
	}
	return t.a.messages.Update(t.genCtx, *t.currentAssistant)
}

func (t *turn) onReasoningDelta(id string, text string) error {
	t.currentAssistant.ActivityDetail = ""
	t.currentAssistant.Activity = "thinking"
	t.currentAssistant.ActivityAt = time.Now().Unix()
	t.currentAssistant.AppendReasoningContent(text)
	if t.progressUpdatesEnabled() && text != "" {
		t.currentAssistant.ShowProgressUpdate(text)
		if strings.TrimSpace(text) != "" {
			t.noteVisibleAssistantText()
		}
		t.currentAssistant.Activity = "responding"
	}
	return t.a.messages.Update(t.genCtx, *t.currentAssistant)
}

func (t *turn) onReasoningEnd(id string, reasoning fantasy.ReasoningContent) error {
	// handle anthropic signature
	if anthropicData, ok := reasoning.ProviderMetadata[anthropic.Name]; ok {
		if reasoning, ok := anthropicData.(*anthropic.ReasoningOptionMetadata); ok {
			if reasoning.RedactedData != "" {
				t.currentAssistant.SetReasoningRedactedData(reasoning.RedactedData)
			} else {
				t.currentAssistant.AppendReasoningSignature(reasoning.Signature)
			}
		}
	}
	if googleData, ok := reasoning.ProviderMetadata[google.Name]; ok {
		if reasoning, ok := googleData.(*google.ReasoningMetadata); ok {
			t.currentAssistant.AppendThoughtSignature(reasoning.Signature, reasoning.ToolID)
		}
	}
	if openaiData, ok := reasoning.ProviderMetadata[openai.Name]; ok {
		if reasoning, ok := openaiData.(*openai.ResponsesReasoningMetadata); ok {
			t.currentAssistant.SetReasoningResponsesData(reasoning)
		}
	}
	if t.progressUpdatesEnabled() {
		blocks := t.currentAssistant.ReasoningBlocks()
		if len(blocks) > 0 && blocks[len(blocks)-1].DisplayTextLength > 0 {
			t.currentAssistant.ShowProgressUpdate("\n\n")
		}
	}
	t.currentAssistant.FinishThinking()
	return t.a.messages.Update(t.genCtx, *t.currentAssistant)
}

func (t *turn) onTextDelta(id string, text string) error {
	t.currentAssistant.ActivityDetail = ""
	t.currentAssistant.Activity = "responding"
	t.currentAssistant.ActivityAt = time.Now().Unix()
	if strings.TrimSpace(text) != "" {
		t.noteVisibleAssistantText()
	}
	// Strip leading newline from initial text content. This is is
	// particularly important in non-interactive mode where leading
	// newlines are very visible.
	if len(t.currentAssistant.Parts) == 0 {
		text = strings.TrimPrefix(text, "\n")
	}

	t.currentAssistant.AppendContent(text)
	return t.a.messages.Update(t.genCtx, *t.currentAssistant)
}

func (t *turn) onToolInputStart(id string, toolName string) error {
	toolCall := message.ToolCall{
		ID:               id,
		Name:             toolName,
		ProviderExecuted: false,
		Finished:         false,
	}
	t.currentAssistant.ActivityDetail = ""
	t.currentAssistant.AddToolCall(toolCall)
	// Use parent ctx instead of genCtx to ensure the update succeeds
	// even if the request is canceled mid-stream
	return t.a.messages.Update(t.ctx, *t.currentAssistant)
}

func (t *turn) onRetry(err *fantasy.ProviderError, delay time.Duration) {
	slog.Warn("Provider request failed, retrying", providerRetryLogFields(err, delay)...)
	// Reset streamed content so the retried response doesn't
	// concatenate with partial content from the failed attempt.
	// On the final attempt (no more retries), any partial content
	// stays in the message as useful context beneath the error.
	t.currentAssistant.ResetStreamedContent()
	t.currentAssistant.Activity = "retrying"
	if err != nil && err.StatusCode == http.StatusTooManyRequests {
		t.currentAssistant.Activity = "rate_limited"
	}
	t.currentAssistant.ActivityDetail = ""
	t.currentAssistant.ActivityAt = time.Now().Unix()
	if updateErr := t.a.messages.Update(t.genCtx, *t.currentAssistant); updateErr != nil {
		slog.Error("Failed to reset message on retry", "error", updateErr)
	}
}

func (t *turn) modelProvider() fantasy.LanguageModel {
	if msg := t.currentAssistant; msg != nil && (msg.Activity == "retrying" || msg.Activity == "rate_limited") {
		msg.Activity = ""
		msg.ActivityAt = time.Now().Unix()
		if err := t.a.messages.Update(t.genCtx, *msg); err != nil {
			slog.Error("Failed to clear retry status", "error", err)
		}
	}
	// The turn's own snapshot, not the agent's: a switch made elsewhere
	// mid-turn must not swap it. The same choice is looked up again, so a
	// sign-in refreshed mid-turn (the retry after a 401) reaches the next
	// request in a model built with the new credentials.
	m := t.chatModel
	slog.Info("ModelProvider called",
		"provider", m.ModelCfg.Provider,
		"model", m.ModelCfg.Model)
	return t.a.currentBuildOf(m)
}

func (t *turn) onToolCall(tc fantasy.ToolCallContent) error {
	t.toolResultMu.Lock()
	defer t.toolResultMu.Unlock()
	t.fileReview.track(tc.ToolCallID, tc.ToolName, tc.Input)
	input, wasSanitized := sanitizeToolInput(tc.ToolName, tc.ToolCallID, tc.Input)
	if wasSanitized {
		t.sanitizedToolCalls.Set(tc.ToolCallID, true)
	}
	toolCall := message.ToolCall{
		ID:               tc.ToolCallID,
		Name:             tc.ToolName,
		Input:            input,
		ProviderExecuted: false,
		Finished:         true,
	}
	t.currentAssistant.AddToolCall(toolCall)
	// Use parent ctx instead of genCtx to ensure the update succeeds
	// even if the request is canceled mid-stream
	return t.a.messages.Update(t.ctx, *t.currentAssistant)
}

// onToolResult records a tool's reply. Fantasy calls it concurrently for
// parallel tool calls, so the file review and transcript writes take
// toolResultMu.
func (t *turn) onToolResult(result fantasy.ToolResultContent) error {
	toolResult := t.a.convertToToolResult(result)
	if t.wasInterruptedForProgress() && toolResult.IsError && strings.Contains(toolResult.Content, context.Canceled.Error()) {
		toolResult.Content = progressInterruptedToolCallResult
	}
	if _, ok := t.sanitizedToolCalls.Get(result.ToolCallID); ok {
		toolResult.Content = "Tool call failed: arguments were not valid JSON. Please check your tool call format and try again."
		toolResult.IsError = true
	}
	if !toolResult.IsError && toolResult.Name != "" {
		t.completedToolCalls.Set(toolResult.Name, struct{}{})
	}
	t.toolResultMu.Lock()
	defer t.toolResultMu.Unlock()
	// Use parent ctx instead of genCtx to ensure the message is created
	// even if the request is canceled mid-stream
	if err := t.fileReview.save(t.ctx, toolResult); err != nil {
		return err
	}
	t.answeredToolCalls.Set(result.ToolCallID, struct{}{})
	shelltool.PersistBackgroundReview(t.ctx, t.a.messages, t.call.SessionID, toolResult)
	return nil
}

// refreshProviderLimits reads a metered provider's plan usage while the
// turn is still working, so its meter moves every tool step. Only
// Moonshot AI reports usage this way: its plan is read over HTTP, so
// nothing in its stream carries new limits.
func (t *turn) refreshProviderLimits() {
	if t.chatModel.ModelCfg.Provider != KimiProviderID {
		return
	}
	cliagent.RefreshLimitsDuringTurn(t.ctx, catwalk.Type(catwalk.InferenceKimiCoding), t.chatModel.ModelCfg.Account)
}

func (t *turn) onStepFinish(stepResult fantasy.StepResult) error {
	a := t.a
	if stepResult.FinishReason == fantasy.FinishReasonToolCalls {
		t.refreshProviderLimits()
	}
	for _, w := range stepResult.Warnings {
		slog.Warn("Provider warning", "type", w.Type, "message", w.Message)
	}
	finishReason, finishMessage, finishDetails := turnFinish(t.chatModel, t.call.SessionID, stepResult.FinishReason)
	// If a tool result halted the turn (e.g. a hook halt or a
	// permission denial), the step ends on FinishReasonToolCalls but
	// the model will not be called again. Treat it as the end of the
	// turn so the UI can render the assistant footer.
	if finishReason == message.FinishReasonToolUse {
		for _, tr := range stepResult.Content.ToolResults() {
			if tr.StopTurn {
				finishReason = message.FinishReasonEndTurn
				break
			}
		}
	}
	t.currentAssistant.AddFinish(finishReason, finishMessage, finishDetails)
	t.currentAssistant.PrismModelID, t.currentAssistant.PrismModelName = extractPrismModel(stepResult.ProviderMetadata)
	t.currentAssistant.PrismHypercreditSavings, t.currentAssistant.PrismDollarSavings = extractPrismSavings(stepResult.ProviderMetadata)
	t.sessionLock.Lock()
	defer t.sessionLock.Unlock()

	usage, estimated := fallbackStepUsage(t.stepMessages, stepResult)
	var updatedSession session.Session
	_, sessionErr := a.sessions.Edit(t.ctx, t.call.SessionID, func(current *session.Session) bool {
		// Priced at the model Prism routed a Hyper step to.
		pricingModel := a.modelForStepCost(t.chatModel, stepResult.ProviderMetadata)
		a.updateSessionUsage(pricingModel, current, usage, a.openrouterCost(stepResult.ProviderMetadata), estimated)
		updatedSession = *current
		return true
	})
	if sessionErr != nil {
		return sessionErr
	}
	t.currentSession = updatedSession
	return a.messages.Update(t.genCtx, *t.currentAssistant)
}

// shouldAutoSummarize stops the turn once the session nears the model's
// context window, so it can be summarized before continuing.
func (t *turn) shouldAutoSummarize(_ []fantasy.StepResult) bool {
	a := t.a
	if t.isCLI && config.NativeCompaction(t.cliModel.Kind) {
		return false
	}
	cw := int64(t.chatModel.CatwalkCfg.ContextWindow)
	tokens := t.currentSession.CompletionTokens + t.currentSession.PromptTokens
	limit := config.DefaultAutoCompactTokenLimit
	var options *config.Options
	if a.cfg != nil {
		options = a.cfg.Config().Options
		limit = options.GetAutoCompactTokenLimit()
	}
	limit = options.PriceStepCompactionLimit(t.chatModel.ModelCfg.Provider, config.FallbackCompactionLimit(cw, limit))
	if tokens >= limit && !a.disableAutoSummarize {
		t.shouldSummarize = true
		return true
	}
	return false
}

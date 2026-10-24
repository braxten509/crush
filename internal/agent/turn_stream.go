package agent

import (
	"context"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"charm.land/fantasy"
	"charm.land/fantasy/providers/anthropic"
	"charm.land/fantasy/providers/google"
	"charm.land/fantasy/providers/openai"

	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/agent/tools/shelltool"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/message"
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
	largeModel := t.largeModel
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
		queued, err := a.takeQueuedForStep(callContext, call, largeModel.CatwalkCfg.SupportsImages)
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

	prepared.Messages = a.workaroundProviderMediaLimitations(prepared.Messages, largeModel)

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
		Model:    largeModel.ModelCfg.Model,
		Provider: largeModel.ModelCfg.Provider,
	})
	if err != nil {
		return callContext, prepared, err
	}
	callContext = context.WithValue(callContext, tools.MessageIDContextKey, assistantMsg.ID)
	callContext = context.WithValue(callContext, tools.ChannelContextKey, call.Channel)
	callContext = context.WithValue(callContext, tools.SupportsImagesContextKey, largeModel.CatwalkCfg.SupportsImages)
	callContext = context.WithValue(callContext, tools.ModelNameContextKey, largeModel.CatwalkCfg.Name)
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

func (t *turn) onReasoningStart(id string, reasoning fantasy.ReasoningContent) error {
	t.currentAssistant.ActivityDetail = ""
	t.currentAssistant.Activity = "thinking"
	t.currentAssistant.ActivityAt = time.Now().Unix()
	t.currentAssistant.FinishThinking()
	t.currentAssistant.StartReasoning(reasoning.Text)
	return t.a.messages.Update(t.genCtx, *t.currentAssistant)
}

func (t *turn) onReasoningDelta(id string, text string) error {
	t.currentAssistant.ActivityDetail = ""
	t.currentAssistant.Activity = "thinking"
	t.currentAssistant.ActivityAt = time.Now().Unix()
	t.currentAssistant.AppendReasoningContent(text)
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
	// The turn's own snapshot, not the agent's: a chat may run on its own
	// model, and a switch made elsewhere mid-turn must not swap it.
	m := t.largeModel
	if m.Model == nil {
		m = t.a.largeModel.Get()
	}
	slog.Info("ModelProvider called",
		"provider", m.ModelCfg.Provider,
		"model", m.ModelCfg.Model)
	return m.Model
}

func (t *turn) onToolCall(tc fantasy.ToolCallContent) error {
	t.fileReview.track(tc.ToolCallID, tc.ToolName, tc.Input)
	input, wasSanitized := sanitizeToolInput(tc.ToolName, tc.ToolCallID, tc.Input)
	if wasSanitized {
		t.sanitizedToolCalls[tc.ToolCallID] = true
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

func (t *turn) onToolResult(result fantasy.ToolResultContent) error {
	toolResult := t.a.convertToToolResult(result)
	if t.sanitizedToolCalls[result.ToolCallID] {
		toolResult.Content = "Tool call failed: arguments were not valid JSON. Please check your tool call format and try again."
		toolResult.IsError = true
	}
	if !toolResult.IsError && toolResult.Name != "" {
		t.completedToolCalls[toolResult.Name] = struct{}{}
	}
	// Use parent ctx instead of genCtx to ensure the message is created
	// even if the request is canceled mid-stream
	if err := t.fileReview.save(t.ctx, toolResult); err != nil {
		return err
	}
	shelltool.PersistBackgroundReview(t.ctx, t.a.messages, t.call.SessionID, toolResult)
	return nil
}

func (t *turn) onStepFinish(stepResult fantasy.StepResult) error {
	a := t.a
	for _, w := range stepResult.Warnings {
		slog.Warn("Provider warning", "type", w.Type, "message", w.Message)
	}
	finishReason := message.FinishReasonUnknown
	switch stepResult.FinishReason {
	case fantasy.FinishReasonLength:
		finishReason = message.FinishReasonMaxTokens
	case fantasy.FinishReasonStop:
		finishReason = message.FinishReasonEndTurn
	case fantasy.FinishReasonToolCalls:
		finishReason = message.FinishReasonToolUse
	case fantasy.FinishReasonContentFilter:
		// Provider safety classifier stopped the model
		// (Anthropic stop_reason=refusal, OpenAI content_filter).
		// The TUI owns the display copy; we only persist the
		// reason so the UI can show a REFUSED banner.
		finishReason = message.FinishReasonContentFilter
		slog.Warn(
			"Provider content filter stopped the model",
			"session_id", t.call.SessionID,
			"finish_reason", string(stepResult.FinishReason),
		)
	}
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
	t.currentAssistant.AddFinish(finishReason, "", "")
	t.currentAssistant.PrismModelID, t.currentAssistant.PrismModelName = extractPrismModel(stepResult.ProviderMetadata)
	t.currentAssistant.PrismHypercreditSavings, t.currentAssistant.PrismDollarSavings = extractPrismSavings(stepResult.ProviderMetadata)
	t.sessionLock.Lock()
	defer t.sessionLock.Unlock()

	updatedSession, getSessionErr := a.sessions.Get(t.ctx, t.call.SessionID)
	if getSessionErr != nil {
		return getSessionErr
	}
	usage, estimated := fallbackStepUsage(t.stepMessages, stepResult)
	a.updateSessionUsage(t.largeModel, &updatedSession, usage, a.openrouterCost(stepResult.ProviderMetadata), estimated)
	_, sessionErr := a.sessions.Save(t.ctx, updatedSession)
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
	cw := int64(t.largeModel.CatwalkCfg.ContextWindow)
	tokens := t.currentSession.CompletionTokens + t.currentSession.PromptTokens
	limit := config.DefaultAutoCompactTokenLimit
	if a.cfg != nil {
		limit = a.cfg.Config().Options.GetAutoCompactTokenLimit()
	}
	if tokens >= config.FallbackCompactionLimit(cw, limit) && !a.disableAutoSummarize {
		t.shouldSummarize = true
		return true
	}
	return false
}

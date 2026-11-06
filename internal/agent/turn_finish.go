package agent

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"charm.land/fantasy"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/exp/charmtone"

	"github.com/charmbracelet/crush/internal/agent/providers"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/stringext"
)

// handleStreamError records a failed stream: it closes the dangling tool
// calls, finishes the assistant message with the classified error, and
// tells the originating channel.
func (t *turn) handleStreamError(result *fantasy.AgentResult, err error) (*fantasy.AgentResult, error) {
	a := t.a
	isCancelErr := errors.Is(err, context.Canceled)
	slog.Info("Agent stream returned error",
		"error", err.Error(),
		"error_type", fmt.Sprintf("%T", err),
		"oauth", t.chatModel.OAuth,
		"is_cancel", isCancelErr)
	// Fire once the turn's final state is written, whichever return below
	// is taken, and outlive the cancelled run context. A turn cut for a
	// progress update goes on in its queued follow-up, which fires it.
	if !t.wasInterruptedForProgress() {
		defer a.fireTurnHook(context.WithoutCancel(t.ctx), t.call, turnEndEvent(err, t.currentAssistant))
	}
	if t.currentAssistant == nil {
		// Cancel-before-assistant-creation window: the run was
		// canceled after activeRequests.Set but before PrepareStep
		// created the assistant message. Without this, the turn
		// would return with no FinishReasonCanceled marker and no
		// user-visible record. The user message was already created
		// above, so persistCanceledTurn only writes the assistant
		// record.
		if isCancelErr {
			if persistErr := a.persistCanceledTurn(t.ctx, t.call, t.userMsgCreated); persistErr != nil {
				return nil, persistErr
			}
		}
		return result, err
	}
	// Persist final state with a context detached from the run
	// context. The run context (ctx) is derived from the
	// workspace context, which workspace shutdown cancels before
	// agent goroutines finish; using ctx here would drop the
	// final assistant state. WithoutCancel keeps the values
	// (e.g. session ID) while ignoring cancellation, and a short
	// timeout bounds the cleanup writes.
	cleanupCtx, cleanupCancel := context.WithTimeout(context.WithoutCancel(t.ctx), 5*time.Second)
	defer cleanupCancel()
	// Ensure we finish thinking on error to close the reasoning state.
	t.currentAssistant.FinishThinking()
	reason, title, details := classifyRunError(err, t.chatModel)
	if t.wasInterruptedForProgress() {
		reason, title, details = message.FinishReasonCanceled, "Interrupted for a progress update", ""
	}
	// Interactive chats continue on their own once a usage limit resets
	// (coordinator.planLimitRetry); say when.
	// The fallback order says for itself where a chat it steers goes.
	if !t.call.NonInteractive && !a.isSubAgent && !fallbackHolds(a.cfg, t.chatModel.ModelCfg) {
		if at, ok := limitRetryTime(err, time.Now()); ok {
			details = strings.TrimSpace(details + "\n\n" + limitRetryNote(at))
		}
	}
	t.currentAssistant.AddFinish(reason, title, details)
	// Note: we use the cleanup context here because the genCtx has been
	// cancelled.
	updateErr := a.messages.Update(cleanupCtx, *t.currentAssistant)
	if updateErr != nil {
		return nil, updateErr
	}
	// A channel-originated turn has no caller watching the error, so
	// tell the channel side something went wrong instead of leaving
	// the sender hanging. sendChannelReply detaches from the run
	// context so the notice survives a provider error that tore it down.
	if channelErrorReplyWanted(t.call.Channel, err) {
		a.sendChannelReply(t.ctx, t.call,
			"Something went wrong while handling your message. Please try again.",
			t.completedToolCalls.Copy())
	}
	return nil, err
}

// progressInterruptedToolCallResult is the reply to a call a progress
// update cut short; the task goes on after the update.
const progressInterruptedToolCallResult = "Tool call interrupted so the agent can update the user before continuing."

// settle answers every call in the turn's last assistant message that has
// no reply yet, so a call can never outlive the turn that made it. Every way
// a native turn ends reaches this; an agent CLI runs its own tools, so its
// turn settles only when it fails. Replies go through the file review,
// which keeps what an interrupted call already changed. Failures are
// logged: the turn's own outcome matters more.
func (t *turn) settle(runErr error) {
	if t.currentAssistant == nil || (t.isCLI && runErr == nil) {
		return
	}
	// Final writes use a context detached from the run: workspace
	// shutdown cancels it before agent goroutines finish, and the turn's
	// ending must still land.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(t.ctx), 5*time.Second)
	defer cancel()
	if err := t.settleOpenToolCalls(ctx, runErr); err != nil {
		slog.Error("Failed to settle unanswered tool calls",
			"session_id", t.call.SessionID, "error", err)
	}
}

func (t *turn) settleOpenToolCalls(ctx context.Context, runErr error) error {
	a := t.a
	t.toolResultMu.Lock()
	defer t.toolResultMu.Unlock()
	var open []message.ToolCall
	for _, tc := range t.currentAssistant.ToolCalls() {
		if _, ok := t.answeredToolCalls.Get(tc.ID); !ok {
			open = append(open, tc)
		}
	}
	if len(open) == 0 {
		return nil
	}
	// A reply may be stored without the callback having seen it saved.
	msgs, err := a.messages.List(ctx, t.currentAssistant.SessionID)
	if err != nil {
		return err
	}
	stored := make(map[string]struct{})
	for _, msg := range msgs {
		for _, tr := range msg.ToolResults() {
			stored[tr.ToolCallID] = struct{}{}
		}
	}
	open = slices.DeleteFunc(open, func(tc message.ToolCall) bool {
		_, ok := stored[tc.ID]
		return ok
	})
	if len(open) == 0 {
		return nil
	}

	reason := turnEndReason(t.currentAssistant, runErr)
	if t.wasInterruptedForProgress() {
		reason = progressInterruptedToolCallResult
	}
	updated := false
	for _, tc := range open {
		slog.Warn("Settling a tool call that will never be answered",
			"session_id", t.currentAssistant.SessionID,
			"tool_call_id", tc.ID,
			"tool_name", tc.Name,
			"reason", reason)
		if !tc.Finished {
			tc.Finished = true
			// Partial arguments are kept when they are whole JSON, so
			// the model sees what it wrote; anything else could not be
			// sent back to the provider.
			if !json.Valid([]byte(tc.Input)) {
				tc.Input = "{}"
			}
			t.currentAssistant.AddToolCall(tc)
			updated = true
		}
	}
	if updated {
		if err := a.messages.Update(ctx, *t.currentAssistant); err != nil {
			return fmt.Errorf("failed to settle tool calls: %w", err)
		}
	}
	for _, tc := range open {
		if err := t.fileReview.save(ctx, message.ToolResult{
			ToolCallID: tc.ID,
			Name:       tc.Name,
			Content:    reason,
			IsError:    true,
		}); err != nil {
			return fmt.Errorf("failed to record settled tool result: %w", err)
		}
		t.answeredToolCalls.Set(tc.ID, struct{}{})
	}
	return nil
}

// classifyRunError turns the error a turn's stream ended with into the
// finish reason, title and details its assistant message records. model is
// the model the turn ran on.
func classifyRunError(err error, model Model) (message.FinishReason, string, string) {
	modelName := model.CatwalkCfg.Name
	var fantasyErr *fantasy.Error
	var providerErr *fantasy.ProviderError
	var requestTimedOutErr *providers.RequestTimeoutError
	const defaultTitle = "Provider Error"
	switch {
	case errors.Is(err, context.Canceled):
		return message.FinishReasonCanceled, "User canceled request", ""
	case errors.As(err, &requestTimedOutErr):
		// Checked before the provider branches so a deadline our own
		// request timeout imposed is never reported as a provider error.
		return message.FinishReasonError, "Request timed out", requestTimedOutErr.UserMessage()
	case model.OAuth && errors.As(err, &providerErr) && providerErr.StatusCode == http.StatusUnauthorized:
		return message.FinishReasonError, "Unauthorized", signInAgainMessage(model)
	case errors.As(err, &providerErr):
		if providerErr.Message == "The requested model is not supported." {
			url := "https://github.com/settings/copilot/features"
			linkStyle := lipgloss.NewStyle().Foreground(charmtone.Guac).Underline(true)
			link := linkStyle.Hyperlink(url, "id=copilot").Render(url)
			return message.FinishReasonError,
				"Copilot model not enabled",
				fmt.Sprintf("%q is not enabled in Copilot. Go to the following page to enable it. Then, wait 5 minutes before trying again. %s", modelName, link)
		}
		return message.FinishReasonError, cmp.Or(stringext.Capitalize(providerErr.Title), defaultTitle), providerErr.Message
	case errors.As(err, &fantasyErr):
		return message.FinishReasonError, cmp.Or(stringext.Capitalize(fantasyErr.Title), defaultTitle), fantasyErr.Message
	case fantasy.IsTransportError(err):
		wrapped := fantasy.NewTransportError(err)
		return message.FinishReasonError, stringext.Capitalize(wrapped.Title), wrapped.Message
	default:
		return message.FinishReasonError, defaultTitle, err.Error()
	}
}

// finish wraps up a turn whose stream succeeded: it summarizes the session
// when it grew too long, queueing a continuation when work was left, and
// replies on the originating channel.
func (t *turn) finish(result *fantasy.AgentResult) (*fantasy.AgentResult, error) {
	a := t.a
	// Finish before summarization or a recursive queued turn starts touching
	// the folder; its changes belong to that turn's own review.
	t.fileReview.finish(t.ctx)
	// A turn cut short for compaction with tool calls still pending is
	// continued by a queued follow-up, which replies and fires TurnEnd
	// when it actually finishes.
	continued := false
	if t.shouldSummarize {
		a.activeRequests.Del(t.call.SessionID)
		summarizeErr := a.summarize(t.genCtx, t.call.SessionID, compactTriggerAuto, t.call.ProviderOptions, t.call.OnAuthRefresh)
		switch {
		case errors.Is(summarizeErr, ErrCompactionBlocked):
			// Uncompacted, the next step would only hit the limit
			// again, so the turn ends here.
			slog.Info("PreCompact hook blocked compaction", "session_id", t.call.SessionID)
		case summarizeErr != nil:
			return nil, summarizeErr
		case t.currentAssistant != nil && len(t.currentAssistant.ToolCalls()) > 0:
			// The agent wasn't done.
			t.queueContinuation()
			continued = true
		}
	}
	finished := t.currentAssistant != nil && !continued

	// Route the finished turn's response back to the channel it came
	// from. Runs before the busy state is released so a follow-up push
	// queued behind this turn cannot overtake its reply. The reply text
	// carries a closing note when the response stopped short, because a
	// channel recipient sees only the text, never the TUI banner.
	if finished {
		a.sendChannelReply(t.ctx, t.call, channelReplyText(t.currentAssistant), t.completedToolCalls.Copy())
		a.fireTurnHook(t.ctx, t.call, turnEndEvent(nil, t.currentAssistant))
	}

	// Terminal publication precedes the deferred atomic batch dispatch.
	t.notifyOnSuccess = true
	return result, nil
}

// queueContinuation queues the rest of a turn that was summarized with
// work left, carrying the originals it took in so the continuation
// completes them.
func (t *turn) queueContinuation() {
	a := t.a
	queueMu := a.sessionMu(t.call.SessionID)
	queueMu.Lock()
	existing, ok := a.messageQueue.Get(t.call.SessionID)
	if !ok {
		existing = []SessionAgentCall{}
	}
	t.call.Prompt = fmt.Sprintf("The previous session was interrupted because it got too long, the initial user request was: `%s`", t.call.Prompt)
	t.call.NotRecallable = true
	t.call.userMessagesCreated = true
	// The rewritten prompt is a new message, not the report.
	t.call.Report = nil
	t.call.onQueuedInput = nil
	if len(t.consumedCalls) > 0 {
		t.call.batch = slices.Concat(t.call.originalCalls(), t.consumedCalls)
		t.call.sessionRun = sharedBatchOwner(t.call.batch)
		t.call.queuedSessionRunRelease = nil
		t.call.OnComplete = nil
		t.consumedCalls = nil
	}
	t.skipRunComplete = true
	t.queuedAgain = true
	t.call.report.handOn()
	existing = append(existing, t.call)
	a.messageQueue.Set(t.call.SessionID, existing)
	queueMu.Unlock()
}

// providerStoppedTurnMessage is the banner title recorded when a provider
// ends a response from its own side without an error reaching the run.
const providerStoppedTurnMessage = "Provider stopped the response"

// turnFinish maps the upstream finish reason of a step onto what the
// assistant message records: the reason itself plus the banner copy the
// agent owns for the stops it has to explain (empty copy means the TUI
// supplies its own). Abnormal stops are logged because they otherwise
// look exactly like a turn that completed normally — the response just
// stops short.
func turnFinish(model Model, sessionID string, reason fantasy.FinishReason) (message.FinishReason, string, string) {
	switch reason {
	case fantasy.FinishReasonStop:
		return message.FinishReasonEndTurn, "", ""
	case fantasy.FinishReasonToolCalls:
		return message.FinishReasonToolUse, "", ""
	case fantasy.FinishReasonLength:
		// The turn looks like a normal, completed answer that simply
		// stops mid-sentence, so leave a trace in the log even though
		// the TUI owns the display copy.
		slog.Warn(
			"Model response truncated at the output token limit",
			"session_id", sessionID,
			"provider", model.ModelCfg.Provider,
			"model", model.ModelCfg.Model,
			"finish_reason", string(reason),
		)
		return message.FinishReasonMaxTokens, "", ""
	case fantasy.FinishReasonContentFilter:
		// Provider safety classifier stopped the model
		// (Anthropic stop_reason=refusal, OpenAI content_filter).
		// The TUI owns the display copy; we only persist the
		// reason so the UI can show a REFUSED banner.
		slog.Warn(
			"Provider content filter stopped the model",
			"session_id", sessionID,
			"finish_reason", string(reason),
		)
		return message.FinishReasonContentFilter, "", ""
	case fantasy.FinishReasonError, fantasy.FinishReasonOther:
		// The provider gave up on the response from its own side
		// (e.g. insufficient_system_resource) but the stream still
		// completed, so no error reaches the run's error path. Record
		// it as an error finish, with copy generic enough for any
		// provider reason, or the turn renders as a normal completed
		// answer that stops short.
		slog.Warn(
			"Provider stopped the model",
			"session_id", sessionID,
			"provider", model.ModelCfg.Provider,
			"model", model.ModelCfg.Model,
			"finish_reason", string(reason),
		)
		return message.FinishReasonError, providerStoppedTurnMessage, fmt.Sprintf(
			"The provider ended this response before it was complete (finish_reason=%s). Try again, or use a different model.",
			string(reason),
		)
	}
	return message.FinishReasonUnknown, "", ""
}

// signInAgainMessage tells the user how to replace a credential the provider
// refused after a refresh.
func signInAgainMessage(m Model) string {
	name := cmp.Or(m.ProviderName, m.ModelCfg.Provider)
	return fmt.Sprintf("Please sign in to %s again. You can also run \"crush login %s --force\".",
		name, m.ModelCfg.Provider)
}

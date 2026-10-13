package agent

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"time"

	"charm.land/fantasy"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/exp/charmtone"

	"github.com/charmbracelet/crush/internal/agent/hyper"
	"github.com/charmbracelet/crush/internal/agent/providers"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/stringext"
)

// handleStreamError records a failed stream: it closes the dangling tool
// calls, finishes the assistant message with the classified error, and
// tells the originating channel.
func (t *turn) handleStreamError(result *fantasy.AgentResult, err error) (*fantasy.AgentResult, error) {
	a := t.a
	isHyper := t.largeModel.ModelCfg.Provider == hyper.Name
	isCancelErr := errors.Is(err, context.Canceled)
	slog.Info("Agent stream returned error",
		"error", err.Error(),
		"error_type", fmt.Sprintf("%T", err),
		"is_hyper", isHyper,
		"is_cancel", isCancelErr)
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
	if err := t.closeDanglingToolCalls(cleanupCtx, isCancelErr); err != nil {
		return nil, err
	}
	reason, title, details := classifyRunError(err, isHyper, t.largeModel.CatwalkCfg.Name)
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
			t.completedToolCalls)
	}
	return nil, err
}

// closeDanglingToolCalls finishes the assistant's unfinished tool calls and
// gives every call without a result an error result, so the history stays
// valid for the next request.
func (t *turn) closeDanglingToolCalls(cleanupCtx context.Context, isCancelErr bool) error {
	a := t.a
	// Ensure we finish thinking on error to close the reasoning state.
	t.currentAssistant.FinishThinking()
	toolCalls := t.currentAssistant.ToolCalls()
	// INFO: we use the cleanup context here because the genCtx has been cancelled.
	msgs, createErr := a.messages.List(cleanupCtx, t.currentAssistant.SessionID)
	if createErr != nil {
		return createErr
	}
	for _, tc := range toolCalls {
		if !tc.Finished {
			tc.Finished = true
			tc.Input = "{}"
			t.currentAssistant.AddToolCall(tc)
			updateErr := a.messages.Update(cleanupCtx, *t.currentAssistant)
			if updateErr != nil {
				return updateErr
			}
		}

		found := false
		for _, msg := range msgs {
			if msg.Role == message.Tool {
				for _, tr := range msg.ToolResults() {
					if tr.ToolCallID == tc.ID {
						found = true
						break
					}
				}
			}
			if found {
				break
			}
		}
		if found {
			continue
		}
		content := "There was an error while executing the tool"
		if isCancelErr {
			content = "Error: user cancelled assistant tool calling"
		}
		toolResult := message.ToolResult{
			ToolCallID: tc.ID,
			Name:       tc.Name,
			Content:    content,
			IsError:    true,
		}
		createErr = t.fileReview.save(cleanupCtx, toolResult)
		if createErr != nil {
			return createErr
		}
	}
	return nil
}

// classifyRunError turns the error a turn's stream ended with into the
// finish reason, title and details its assistant message records.
// modelName names the model in errors about it.
func classifyRunError(err error, isHyper bool, modelName string) (message.FinishReason, string, string) {
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
	case isHyper && errors.As(err, &providerErr) && providerErr.StatusCode == http.StatusUnauthorized:
		return message.FinishReasonError, "Unauthorized", `Please re-authenticate with Hyper. You can also run "crush auth" to re-authenticate.`
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
	if t.shouldSummarize {
		a.activeRequests.Del(t.call.SessionID)
		if summarizeErr := a.Summarize(t.genCtx, t.call.SessionID, t.call.ProviderOptions, t.call.OnAuthRefresh); summarizeErr != nil {
			return nil, summarizeErr
		}
		// If the agent wasn't done...
		if len(t.currentAssistant.ToolCalls()) > 0 {
			t.queueContinuation()
		}
	}

	// Route the finished turn's response back to the channel it came
	// from, unless the turn was cut short for summarization with work
	// still pending — the queued continuation carries the channel and
	// replies when it actually finishes. Runs before the busy state is
	// released so a follow-up push queued behind this turn cannot
	// overtake its reply.
	if t.currentAssistant != nil && (!t.shouldSummarize || len(t.currentAssistant.ToolCalls()) == 0) {
		a.sendChannelReply(t.ctx, t.call, t.currentAssistant.Content().String(), t.completedToolCalls)
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
	existing = append(existing, t.call)
	a.messageQueue.Set(t.call.SessionID, existing)
	queueMu.Unlock()
}

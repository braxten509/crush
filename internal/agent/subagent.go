package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/agent/hyper"
	"github.com/charmbracelet/crush/internal/agent/notify"
	"github.com/charmbracelet/crush/internal/agent/providers"
	"github.com/charmbracelet/crush/internal/pubsub"
)

// subAgentParams holds the parameters for running a sub-agent.
type subAgentParams struct {
	Agent          SessionAgent
	SessionID      string
	AgentMessageID string
	ToolCallID     string
	Prompt         string
	SessionTitle   string
	// SessionSetup is an optional callback invoked after session creation
	// but before agent execution, for custom session configuration.
	SessionSetup func(sessionID string)
}

// runSubAgent runs a sub-agent and handles session management and cost accumulation.
// It creates a sub-session, runs the agent with the given prompt, and propagates
// the cost to the parent session.
func (c *coordinator) runSubAgent(ctx context.Context, params subAgentParams) (fantasy.ToolResponse, error) {
	// Create sub-session
	agentToolSessionID := c.sessions.CreateAgentToolSessionID(params.AgentMessageID, params.ToolCallID)
	session, err := c.sessions.CreateTaskSession(ctx, agentToolSessionID, params.SessionID, params.SessionTitle)
	if err != nil {
		return fantasy.ToolResponse{}, fmt.Errorf("create session: %w", err)
	}

	// Call session setup function if provided
	if params.SessionSetup != nil {
		params.SessionSetup(session.ID)
	}

	// The sub-agent runs on its own chat's model, which starts as its
	// parent chat's (owner, 2026-10-24: every chat owns its model).
	model, err := params.Agent.ChatModel(ctx, session.ID)
	if err != nil {
		return fantasy.ToolResponse{}, err
	}
	maxTokens := subAgentMaxTokens(model)

	providerCfg, ok := c.cfg.Config().Providers.Get(model.ModelCfg.Provider)
	if !ok {
		return fantasy.ToolResponse{}, errModelProviderNotConfigured
	}
	// Include failed and recovery attempts in the parent's cost as well.
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := c.updateParentSessionCost(cleanupCtx, session.ID, params.SessionID); err != nil {
			slog.Warn("Failed to update parent session cost", "child_session", session.ID, "parent_session", params.SessionID, "error", err)
		}
	}()

	// Run the agent
	run := func() (*fantasy.AgentResult, error) {
		return runSubAgentReport(ctx, params.Agent, SessionAgentCall{
			SessionID:        session.ID,
			Prompt:           params.Prompt,
			MaxOutputTokens:  maxTokens,
			ProviderOptions:  getProviderOptions(model, providerCfg),
			Temperature:      model.ModelCfg.Temperature,
			TopP:             model.ModelCfg.TopP,
			TopK:             providers.CallTopK(providerCfg, model.ModelCfg.TopK),
			FrequencyPenalty: model.ModelCfg.FrequencyPenalty,
			PresencePenalty:  model.ModelCfg.PresencePenalty,
			NonInteractive:   true,
			OnAuthRefresh:    c.makeAuthRefreshCallback(providerCfg),
		})
	}
	result, err := run()
	// Notify only if still unauthorized after retry. AWS SSO is handled
	// transparently inside OnAuthRefresh, so it needs no post-run notice.
	if err != nil && isUnauthorized(err) && c.notify != nil && model.ModelCfg.Provider == hyper.Name {
		c.notify.Publish(pubsub.CreatedEvent, notify.Notification{
			Type:       notify.TypeReAuthenticate,
			ProviderID: model.ModelCfg.Provider,
		})
	}
	if errors.Is(err, errSubAgentNoOutput) {
		return fantasy.NewTextErrorResponse("Sub-agent completed but produced no text output."), nil
	}
	if err != nil {
		return fantasy.NewTextErrorResponse(fmt.Sprintf("Failed to generate response: %s", err)), nil
	}

	output := subAgentOutput(result)
	return fantasy.NewTextResponse(output), nil
}

func subAgentMaxTokens(model Model) int64 {
	if model.ModelCfg.MaxTokens != 0 {
		return model.ModelCfg.MaxTokens
	}
	return model.CatwalkCfg.DefaultMaxTokens
}

// reportRecoveryPrompt asks a worker that ran out of output tokens for the
// report it could not finish. It has only what is already in the conversation.
const reportRecoveryPrompt = "Your previous response reached max_tokens before it finished. " +
	"Use only the work already in this conversation; do not restart the task or call tools. " +
	"Reply now with a complete, concise final report (under 600 words): what you verified, what you found, and what is unfinished or uncertain. " +
	"If little or no work was recorded, say so plainly instead of guessing. Do not include your reasoning."

// errSubAgentNoOutput is a run that ended without any report text.
var errSubAgentNoOutput = errors.New("sub-agent completed but produced no text output")

// terminalFinishReason is how the run's last step ended. AgentResult.Response
// is the latest step that has text, so after a last step with none (a tool
// call, or reasoning that ran out of tokens) it carries an earlier step's
// reason instead.
func terminalFinishReason(result *fantasy.AgentResult) fantasy.FinishReason {
	if n := len(result.Steps); n > 0 {
		return result.Steps[n-1].FinishReason
	}
	return result.Response.FinishReason
}

// runSubAgentReport runs a worker and makes sure it ends with a report. A
// worker that runs out of output tokens gets one chance to report its existing
// work, in the same session and with the same model, effort and output
// ceiling. Tool choice none forbids more tool calls while the tool definitions
// stay, which providers need to accept the tool-call history. It never retries
// a refusal or restarts the task (and its side effects).
//
// A second exhaustion, a refusal, a provider error, a run cut off with tool
// calls pending, and a missing report are errors. An unknown or other finish
// reason with a report is not: providers differ in what they send.
func runSubAgentReport(ctx context.Context, agent SessionAgent, call SessionAgentCall) (*fantasy.AgentResult, error) {
	result, err := runToReport(ctx, agent, call)
	if err != nil {
		return result, err
	}
	if result != nil && terminalFinishReason(result) == fantasy.FinishReasonLength {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		call.Prompt = reportRecoveryPrompt
		call.Attachments = nil
		call.reportOnly = true
		if result, err = runToReport(ctx, agent, call); err != nil {
			return result, err
		}
	}
	if result != nil {
		switch reason := terminalFinishReason(result); reason {
		case fantasy.FinishReasonLength:
			return result, errors.New("sub-agent reached max_tokens again while recovering its final report; task incomplete (continue the task to resume)")
		case fantasy.FinishReasonContentFilter:
			return result, errors.New("sub-agent stopped by provider content_filter/refusal; no complete report (not retried)")
		case fantasy.FinishReasonError:
			return result, fmt.Errorf("sub-agent stopped with finish reason %q; no complete report", reason)
		case fantasy.FinishReasonToolCalls:
			return result, errors.New("sub-agent stopped before finishing (its last step still had tool calls: loop detection, a halted tool, or a session summarized for length); no complete report (see its session for the work so far, or continue the task)")
		}
	}
	if strings.TrimSpace(subAgentOutput(result)) == "" {
		return result, errSubAgentNoOutput
	}
	return result, nil
}

func subAgentOutput(result *fantasy.AgentResult) string {
	if result == nil {
		return ""
	}
	return result.Response.Content.Text()
}

// updateParentSessionCost accumulates the cost from a child session to its parent session.
func (c *coordinator) updateParentSessionCost(ctx context.Context, childSessionID, parentSessionID string) error {
	childSession, err := c.sessions.Get(ctx, childSessionID)
	if err != nil {
		return fmt.Errorf("get child session: %w", err)
	}

	parentSession, err := c.sessions.Get(ctx, parentSessionID)
	if err != nil {
		return fmt.Errorf("get parent session: %w", err)
	}

	parentSession.Cost += childSession.Cost

	if _, err := c.sessions.Save(ctx, parentSession); err != nil {
		return fmt.Errorf("save parent session: %w", err)
	}

	return nil
}

// reportBox carries the result of the turn that finally ends a call. A turn
// summarized with work left hands its call on to a continuation that runs
// after it returns, and the worker's real answer is in that later turn.
type reportBox struct {
	once     sync.Once
	done     chan struct{}
	handedOn atomic.Bool
	result   *fantasy.AgentResult
	err      error
}

func newReportBox() *reportBox { return &reportBox{done: make(chan struct{})} }

// handOn records that a later turn, not this one, will end the call.
func (b *reportBox) handOn() {
	if b != nil {
		b.handedOn.Store(true)
	}
}

// fill keeps the first result; later ones (a batch's other originals) are
// the same turn's.
func (b *reportBox) fill(result *fantasy.AgentResult, err error) {
	if b == nil {
		return
	}
	b.once.Do(func() {
		b.result, b.err = result, err
		close(b.done)
	})
}

// runToReport runs one worker call and returns the result of the turn that
// finally ended it, waiting for a continuation when the call was handed on.
// Stopping the worker (ctx) stops the wait.
func runToReport(ctx context.Context, agent SessionAgent, call SessionAgentCall) (*fantasy.AgentResult, error) {
	box := newReportBox()
	call.report = box
	result, err := agent.Run(ctx, call)
	select {
	case <-box.done:
		return box.result, box.err
	default:
	}
	if err != nil || !box.handedOn.Load() {
		return result, err
	}
	select {
	case <-box.done:
		return box.result, box.err
	case <-ctx.Done():
		return result, ctx.Err()
	}
}

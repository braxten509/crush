package agent

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/agent/hyper"
	"github.com/charmbracelet/crush/internal/agent/notify"
	"github.com/charmbracelet/crush/internal/agent/providers"
	"github.com/charmbracelet/crush/internal/agent/tools/mcp"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/pubsub"
)

// Run implements Coordinator.
func (c *coordinator) Run(ctx context.Context, sessionID string, prompt string, attachments ...message.Attachment) (*fantasy.AgentResult, error) {
	return c.run(ctx, nil, sessionID, prompt, attachments...)
}

// RunAccepted implements Coordinator.
func (c *coordinator) RunAccepted(ctx context.Context, accept *AcceptedRun, sessionID string, prompt string, attachments ...message.Attachment) (*fantasy.AgentResult, error) {
	return c.run(ctx, accept, sessionID, prompt, attachments...)
}

type nonInteractiveKey struct{}

// WithNonInteractive marks a prompt to an interactive coordinator as a
// non-interactive one, as server-mode `crush run` sends it to a workspace
// an app has open. It runs as a non-interactive coordinator's prompts do:
// without interactive-only tools, after MCP servers connect, and in a run
// that ends only after its helpers, jobs and questions. The run's follow-ups
// keep the mark through its context.
func WithNonInteractive(ctx context.Context) context.Context {
	return context.WithValue(ctx, nonInteractiveKey{}, true)
}

// interactiveTurn reports whether work started with ctx is interactive:
// the coordinator is, and ctx has no [WithNonInteractive] mark.
func (c *coordinator) interactiveTurn(ctx context.Context) bool {
	return c.interactive && ctx.Value(nonInteractiveKey{}) == nil
}

// run is the shared implementation behind Run and RunAccepted. When
// accept is non-nil it is threaded onto the SessionAgentCall as
// Accepted so sessionAgent.Run can consume the accept reservation under
// dispatchMu; when nil (the in-process/local path) no accept tracking
// applies.
func (c *coordinator) run(ctx context.Context, accept *AcceptedRun, sessionID string, prompt string, attachments ...message.Attachment) (*fantasy.AgentResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c.tasks == nil {
		return c.runTurn(ctx, accept, sessionID, prompt, attachments...)
	}
	var run *sessionRun
	owner := false
	if c.interactiveTurn(ctx) {
		// A marked prompt's run owns its chat until its follow-ups end, so a
		// turn the agent starts there on its own, such as an agent CLI's
		// reply between turns, joins it, as on a non-interactive coordinator.
		// Such a turn has no submission ID; an app's prompt always has one
		// (backend.SendMessage) and runs as its own turn.
		if message.SubmissionID(ctx) == "" {
			run = c.tasks.joinSessionRun(sessionID)
		}
		if run == nil {
			return c.runTurn(ctx, accept, sessionID, prompt, attachments...)
		}
	} else {
		run, owner = c.tasks.beginSessionRun(ctx, sessionID)
	}
	if !owner {
		var turnCtx context.Context = sessionFollowUpContext{Context: run.ctx, values: ctx}
		if sessionID != run.sessionID {
			// Descendant sessions share cancellation and lifetime, but
			// cannot complete the parent's externally correlated request.
			turnCtx = WithRunID(turnCtx, "")
		} else if run.observe(RunIDFromContext(ctx)) {
			turnCtx = WithRunID(turnCtx, RunIDFromContext(ctx))
			MarkRunCompletePublished(ctx)
		}
		ownTurn := sessionID == run.sessionID
		if ownTurn {
			run.turnStarted()
		}
		result, err := c.runTurn(turnCtx, accept, sessionID, prompt, attachments...)
		if ownTurn {
			run.turnEnded()
		}
		run.release(err)
		return result, err
	}
	defer c.tasks.endSessionRun(run)
	run.turnStarted()
	result, err := c.runTurn(run.ctx, accept, sessionID, prompt, attachments...)
	run.turnEnded()
	run.release(err)
	complete, hasComplete, err := run.wait()
	if err != nil {
		complete.SessionID = sessionID
		hasComplete = true
	}
	if run.ctx.Err() != nil {
		c.currentAgent().Cancel(sessionID)
		err = run.ctx.Err()
		complete.SessionID = sessionID
		complete.Cancelled = true
		hasComplete = true
	}
	if hasComplete {
		complete.RunID = RunIDFromContext(ctx)
		if err != nil {
			complete.Error = err.Error()
		}
		if c.runComplete != nil {
			publishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			c.runComplete.PublishMustDeliver(publishCtx, pubsub.UpdatedEvent, complete)
			for _, runID := range run.observerIDs() {
				observed := complete
				observed.RunID = runID
				c.runComplete.PublishMustDeliver(publishCtx, pubsub.UpdatedEvent, observed)
			}
			MarkRunCompletePublished(ctx)
		}
		if err == nil && run.followUps {
			if result == nil {
				result = &fantasy.AgentResult{}
			}
			result.Response.Content = fantasy.ResponseContent{fantasy.TextContent{Text: complete.Text}}
		}
	}
	return result, err
}

func (c *coordinator) runTurn(ctx context.Context, accept *AcceptedRun, sessionID string, prompt string, attachments ...message.Attachment) (result *fantasy.AgentResult, err error) {
	defer c.recoverTurn(ctx, sessionID, &err)
	if err := c.awaitTurnReady(ctx); err != nil {
		return nil, err
	}

	// refresh models before each run. Snapshot the agent first: the run,
	// its model settings, and the model refresh below must all target the
	// same agent even if SetMainAgent swaps the main agent mid-flight.
	agent, agentName := c.activeAgent()
	if err := c.updateAgentModels(ctx, agent, agentName); err != nil {
		return nil, fmt.Errorf("failed to update models: %w", err)
	}

	model := agent.Model()
	call, err := c.turnModelSettings(ctx, model)
	if err != nil {
		return nil, err
	}

	// Coalesce per-attempt RunComplete payloads so only the final
	// outcome reaches subscribers. Without this, the first attempt's
	// failed RunComplete (unauthorized) would race ahead of the
	// retry's success, and `crush run` would exit on the stale error
	// before ever seeing the retry result. Each attempt's
	// SessionAgentCall.OnComplete hook overwrites latest; we publish
	// exactly once after retries resolve, via PublishMustDeliver, so
	// a momentarily-full subscriber buffer can't silently drop the
	// terminal event.
	var (
		latest    notify.RunComplete
		hasLatest bool
	)
	onComplete := func(rc notify.RunComplete) {
		latest = rc
		hasLatest = true
	}
	// Unlike the per-turn retry closure, the session owner outlives queued
	// dispatch. Its callback can safely stay on calls after enqueueing.
	sessionRun := sessionRunFromContext(ctx, sessionID)
	if sessionRun != nil {
		onComplete = sessionRun.record
	}
	// Propagate the caller-supplied RunID (set via agent.WithRunID
	// at the HTTP boundary in backend.SendMessage) onto the
	// SessionAgentCall so the terminal RunComplete event echoes it
	// back. Both attempts in the retry chain reuse the same RunID;
	// the coalesce closure publishes the final outcome under that
	// same correlator.
	runID := RunIDFromContext(ctx)
	channel := ChannelFromContext(ctx)
	c.syncSessionChannel(ctx, sessionID, channel)
	call.SessionID = sessionID
	call.SubmissionID = message.SubmissionID(ctx)
	call.RunID = runID
	call.Channel = channel
	call.Prompt = prompt
	call.HiddenUserMessage = message.HiddenUserMessage(ctx)
	call.CLIContinue = ctx.Value(cliContinueKey{}) != nil
	call.Attachments = attachments
	call.OnComplete = onComplete
	call.Accepted = accept
	call.NonInteractive = !c.interactiveTurn(ctx)
	call.sessionRun = sessionRun

	beforeLoaded := c.skillTracker.LoadedNames()
	result, originalErr := agent.Run(ctx, call)
	logTurnSkillUsage(sessionID, prompt, c.activeSkills, c.skillTracker, beforeLoaded)

	// Notify only if still unauthorized after retry — a successful
	// retry means the user doesn't need to re-authenticate. AWS SSO is
	// handled transparently inside OnAuthRefresh, so it needs no post-run
	// notification here.
	if originalErr != nil && isUnauthorized(originalErr) && c.notify != nil && model.ModelCfg.Provider == hyper.Name {
		c.notify.Publish(pubsub.CreatedEvent, notify.Notification{
			Type:       notify.TypeReAuthenticate,
			ProviderID: model.ModelCfg.Provider,
		})
	}

	if hasLatest && c.runComplete != nil && sessionRunFromContext(ctx, sessionID) == nil {
		c.runComplete.PublishMustDeliver(ctx, pubsub.UpdatedEvent, latest)
		// Signal to the dispatcher (backend.runAgent) that the
		// authoritative terminal RunComplete for this run was already
		// emitted, so it does not publish a duplicate fallback for the
		// error it is about to receive.
		MarkRunCompletePublished(ctx)
	}
	return result, originalErr
}

// awaitTurnReady waits for the coordinator to finish starting and, for
// non-interactive runs, for MCP servers to connect.
func (c *coordinator) awaitTurnReady(ctx context.Context) error {
	if err := c.readyWg.Wait(); err != nil {
		return err
	}

	// MCP servers connect asynchronously (see mcp.Initialize).
	//
	// Interactive runs never wait for that to finish: the tool list below
	// is built from whatever is registered right now, servers still
	// connecting are simply absent from this run's palette, and they are
	// picked up by later runs once they register and publish
	// EventToolsListChanged. Blocking here froze the TUI for the duration
	// of the slowest server's connect timeout whenever a prompt was sent
	// before initialization finished — most visibly on the first message.
	//
	// Non-interactive runs get a single shot at the tool palette, so they
	// do wait for initialization to settle — but bounded by InitWaitBudget
	// rather than each server's connect timeout, so a server wedged
	// mid-handshake cannot stall a headless run for minutes. Past the
	// budget the turn proceeds without the stragglers; their tools simply
	// stay absent from this run.
	if !c.interactiveTurn(ctx) {
		if err := mcp.WaitForInitBudget(ctx, mcp.InitWaitBudget); err != nil {
			return fmt.Errorf("failed to wait for MCP initialization: %w", err)
		}
	}
	return nil
}

// turnModelSettings returns a call carrying the model's output limit,
// sampling options and auth refresh, refreshing an expired OAuth token
// first.
func (c *coordinator) turnModelSettings(ctx context.Context, model Model) (SessionAgentCall, error) {
	maxTokens := model.CatwalkCfg.DefaultMaxTokens
	if model.ModelCfg.MaxTokens != 0 {
		maxTokens = model.ModelCfg.MaxTokens
	}

	providerCfg, ok := c.cfg.Config().Providers.Get(model.ModelCfg.Provider)
	if !ok {
		return SessionAgentCall{}, errModelProviderNotConfigured
	}

	mergedOptions, temp, topP, topK, freqPenalty, presPenalty := mergeCallOptions(model, providerCfg)

	if err := c.refreshTokenIfExpired(ctx, providerCfg); err != nil {
		// NOTE(@andreynering): We don't return here because the event handling to ask the user to reauthenticate
		// depends on the flow below. If refresh fails, proceed with the token we have.
		slog.Error("Failed to refresh OAuth2 token. Proceeding with existing token.", "error", err)
	}

	return SessionAgentCall{
		MaxOutputTokens:  maxTokens,
		ProviderOptions:  mergedOptions,
		Temperature:      temp,
		TopP:             topP,
		TopK:             providers.CallTopK(providerCfg, topK),
		FrequencyPenalty: freqPenalty,
		PresencePenalty:  presPenalty,
		OnAuthRefresh:    c.makeAuthRefreshCallback(providerCfg),
	}, nil
}

// syncSessionChannel reconciles the session's persisted channel binding with
// the origin of the turn about to run. A channel-originated turn (re)binds
// the session to that channel — the newest push wins — and a local turn
// clears a stale binding, since the session is no longer channel-driven once
// the user takes it over directly. This is the binding's whole lifecycle:
// it is only ever a reflection of the most recent turn's origin, and the
// column is dropped with the session row when the session is deleted, so
// there is no separate state to reap.
//
// Failures are logged and the turn proceeds: the binding is provenance for
// reply routing, not a precondition for running.
func (c *coordinator) syncSessionChannel(ctx context.Context, sessionID, channel string) {
	sess, err := c.sessions.Get(ctx, sessionID)
	if err != nil {
		// A missing session is expected (it may be created later in the
		// run), but a real database failure would otherwise be silent.
		if !errors.Is(err, sql.ErrNoRows) {
			slog.Warn("Failed to load session for channel binding sync",
				"session", sessionID, "channel", channel, "error", err)
		}
		return
	}
	if sess.Channel == channel {
		return
	}
	if _, err := c.sessions.SetChannel(ctx, sessionID, channel); err != nil {
		slog.Warn("Failed to sync session channel binding",
			"session", sessionID, "channel", channel, "error", err)
	}
}

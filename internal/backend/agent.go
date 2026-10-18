package backend

import (
	"context"
	"errors"
	"os"
	"sync"

	"github.com/charmbracelet/crush/internal/agent"
	"github.com/charmbracelet/crush/internal/agent/notify"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/google/uuid"
)

// SendMessage validates and accepts a prompt for the workspace's agent,
// then dispatches the run on a goroutine bound to the workspace context
// and returns immediately. It does not wait for the LLM turn to
// complete: the run's lifetime is owned by the workspace, not by the
// caller. Errors from the dispatched run reach observers through the
// agent event channels (a notify.TypeAgentError notification), not
// through this return value.
//
// SendMessage returns synchronously when the request cannot be accepted:
// ErrWorkspaceNotFound if the workspace is missing, ErrAgentNotInitialized
// if its coordinator is nil, the structural validation errors from
// agent.ValidateCall (ErrEmptyPrompt, ErrSessionMissing) when the prompt
// or session is missing, and ErrWorkspaceClosing if the workspace is
// being torn down.
//
// clientID names the app that sent the prompt, or "" for none.
func (b *Backend) SendMessage(workspaceID, clientID string, msg proto.AgentMessage) error {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return err
	}

	if ws.AgentCoordinator == nil {
		return ErrAgentNotInitialized
	}

	if err := agent.ValidateCall(agent.SessionAgentCall{
		SessionID:   msg.SessionID,
		Prompt:      msg.Prompt,
		Attachments: proto.AttachmentsToMessage(msg.Attachments),
	}); err != nil {
		return err
	}

	// Every prompt gets a submission ID, so its app is known until its
	// RunComplete (engine-design.md 5.5).
	if msg.SubmissionID == "" {
		msg.SubmissionID = uuid.NewString()
	}

	accept := ws.AgentCoordinator.BeginAccepted(msg.SessionID)

	ws.runMu.Lock()
	if ws.closing {
		ws.runMu.Unlock()
		accept.Close()
		return ErrWorkspaceClosing
	}
	ws.runWG.Add(1)
	ws.runMu.Unlock()

	b.runHooks(func(h clientHooks) {
		if h.messageSent != nil {
			h.messageSent(ws, clientID, msg)
		}
	})
	go b.runAgent(ws, msg, accept)
	return nil
}

// runAgent executes an accepted agent run for the workspace. It owns the
// accept reservation (releasing it on return) and the runWG ticket added
// by SendMessage. The run is bound to the workspace context so its
// lifetime is independent of any client's HTTP request.
//
// On a non-cancel error it surfaces the failure to observers via a
// notify.TypeAgentError notification (lossy, best-effort). That alone is
// not a reliable terminal signal: the agent-event fan-in uses lossy
// subscribers, so a `crush run` caller blocking on its RunID could hang
// if the event is dropped. To guarantee termination, when msg.RunID is
// non-empty and the coordinator did not already publish the run's
// authoritative terminal RunComplete (e.g. the error was returned before
// sessionAgent.Run executed, such as a readyWg or UpdateModels failure),
// runAgent emits an errored RunComplete on the must-deliver
// runCompletions broker so the waiter observes a deterministic terminal
// event. context.Canceled is expected (sessionAgent.Run already
// publishes the cancelled terminal marker) and produces no error
// terminal event.
//
// When msg.RunID is non-empty it is attached to the context via
// agent.WithRunID so the coordinator can stamp the terminal
// notify.RunComplete event with that correlator. A run-complete marker
// is also attached so the coordinator can report whether it published
// the terminal event, letting runAgent avoid a duplicate fallback.
func (b *Backend) runAgent(ws *Workspace, msg proto.AgentMessage, accept *agent.AcceptedRun) {
	defer ws.runWG.Done()
	defer accept.Close()

	ctx := ws.ctx
	ctx = message.WithSubmissionID(ctx, msg.SubmissionID)
	if msg.HiddenUserMessage {
		ctx = message.WithHiddenUserMessage(ctx)
	}
	if msg.RunID != "" {
		ctx = agent.WithRunID(ctx, msg.RunID)
	}
	ctx = agent.WithRunCompleteMarker(ctx)

	if msg.Channel != "" {
		ctx = agent.WithChannel(ctx, msg.Channel)
	}
	_, err := ws.AgentCoordinator.RunAccepted(ctx, accept, msg.SessionID, msg.Prompt, proto.AttachmentsToMessage(msg.Attachments)...)
	if err == nil || errors.Is(err, context.Canceled) {
		return
	}

	ws.AgentNotifications().Publish(pubsub.CreatedEvent, notify.Notification{
		SessionID: msg.SessionID,
		RunID:     msg.RunID,
		Type:      notify.TypeAgentError,
		Message:   err.Error(),
	})

	// Reliable terminal fallback. Only needed when a RunID waiter
	// exists and the coordinator has not already emitted the run's
	// terminal RunComplete; otherwise this would be a duplicate.
	if (msg.RunID == "" && msg.SubmissionID == "") || agent.RunCompletePublished(ctx) {
		return
	}
	if rc := ws.RunCompletions(); rc != nil {
		rc.PublishMustDeliver(ctx, pubsub.UpdatedEvent, notify.RunComplete{
			SubmissionID: msg.SubmissionID,
			SessionID:    msg.SessionID,
			RunID:        msg.RunID,
			Error:        err.Error(),
		})
	}
}

// GetAgentInfo returns the agent's model and busy status.
func (b *Backend) GetAgentInfo(workspaceID string) (proto.AgentInfo, error) {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return proto.AgentInfo{}, err
	}

	var agentInfo proto.AgentInfo
	if ws.AgentCoordinator != nil {
		m := ws.AgentCoordinator.Model()
		agentInfo = proto.AgentInfo{
			Model:    m.CatwalkCfg,
			ModelCfg: m.ModelCfg,
			IsBusy:   ws.AgentCoordinator.IsBusy(),
			IsReady:  true,
		}
	}
	return agentInfo, nil
}

// InitAgent initializes the coder agent for the workspace. clientID names
// the app that asked, or "" for none.
func (b *Backend) InitAgent(ctx context.Context, workspaceID, clientID string, interactive bool) error {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return err
	}

	if err := initCoder(ctx, ws, interactive); err != nil {
		return err
	}
	b.runHooks(func(h clientHooks) {
		if h.agentInit != nil {
			h.agentInit(ws, clientID, interactive)
		}
	})
	if interactive {
		// After the hooks, so a resumed helper's owner is this app (5.5).
		agent.ResumeInterruptedTasks()
	}
	return nil
}

// initCoder starts the workspace's coder agent. Every start, non-interactive
// ones too, keeps the interactive coordinator the workspace already runs
// (the one app.New started, or an earlier app's): a workspace has one
// coordinator shared by every app (engine-design.md 5.2), so another app's
// turns, queue and helpers stay on it, and no replaced coordinator's helper
// hub is left listed to claim saved helpers. A non-interactive start makes
// its own coordinator only when no interactive one runs.
func initCoder(ctx context.Context, ws *Workspace, interactive bool) error {
	l := &ws.live
	l.initMu.Lock()
	defer l.initMu.Unlock()
	if ws.coordinator() != nil && !l.nonInteractive {
		return nil
	}
	if !interactive {
		if err := ws.InitCoderAgentNonInteractive(ctx); err != nil {
			return err
		}
		l.nonInteractive = true
		return nil
	}
	if err := ws.InitCoderAgent(ctx); err != nil {
		return err
	}
	l.nonInteractive = false
	return nil
}

// UpdateAgent reloads the agent model configuration.
func (b *Backend) UpdateAgent(ctx context.Context, workspaceID string) error {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return err
	}

	return ws.UpdateAgentModel(ctx)
}

// SetMainAgent switches the workspace's active agent (coder or plan). It
// is rejected while the agent is running so a switch can never strand a
// run's queued prompts on the previous agent — the same protection the
// TUI gives itself with its busy check before toggling input mode.
func (b *Backend) SetMainAgent(workspaceID, agentID string) error {
	o, err := b.ops(workspaceID)
	if err != nil {
		return err
	}
	return o.SetMainAgent(agentID)
}

// CancelSession cancels an ongoing agent operation for the given
// session.
func (b *Backend) CancelSession(workspaceID, sessionID string) error {
	o, err := b.ops(workspaceID)
	if err != nil {
		return err
	}
	o.CancelSession(sessionID)
	return nil
}

// SummarizeSession triggers a session summarization.
func (b *Backend) SummarizeSession(ctx context.Context, workspaceID, sessionID string) error {
	o, err := b.ops(workspaceID)
	if err != nil {
		return err
	}
	return o.Summarize(ctx, sessionID)
}

// QueuedPrompts returns the number of queued prompts for the session.
func (b *Backend) QueuedPrompts(workspaceID, sessionID string) (int, error) {
	o, err := b.ops(workspaceID)
	if err != nil {
		return 0, err
	}
	return o.QueuedPrompts(sessionID), nil
}

// ClearQueue clears the prompt queue for the session.
func (b *Backend) ClearQueue(workspaceID, sessionID string) error {
	o, err := b.ops(workspaceID)
	if err != nil {
		return err
	}
	o.ClearQueue(sessionID)
	return nil
}

type retainedRecall struct {
	mu      sync.Mutex
	claimed bool
	prompt  *proto.AgentMessage
}

func (b *Backend) recalledPrompt(workspaceID, sessionID, requestID string) *retainedRecall {
	b.recallMu.Lock()
	defer b.recallMu.Unlock()
	if b.recalledPrompts == nil {
		b.recalledPrompts = make(map[string]*retainedRecall)
	}
	key := workspaceID + "\x00" + sessionID + "\x00" + requestID
	entry := b.recalledPrompts[key]
	if entry == nil {
		entry = &retainedRecall{}
		b.recalledPrompts[key] = entry
	}
	return entry
}

// RecallQueuedPrompt atomically withdraws a prompt and retains it until delivery.
func (b *Backend) RecallQueuedPrompt(workspaceID, sessionID, requestID string) (*proto.AgentMessage, error) {
	o, err := b.ops(workspaceID)
	if err != nil {
		return nil, err
	}
	if o.App.AgentCoordinator == nil {
		return nil, ErrAgentNotInitialized
	}
	entry := b.recalledPrompt(workspaceID, sessionID, requestID)
	entry.mu.Lock()
	defer entry.mu.Unlock()
	if !entry.claimed {
		prompt, err := o.RecallQueuedPrompt(sessionID)
		if err != nil {
			return nil, err
		}
		if prompt != nil {
			entry.prompt = &proto.AgentMessage{SessionID: sessionID, Prompt: prompt.Prompt, Attachments: proto.AttachmentsFromMessage(prompt.Attachments), SubmissionID: prompt.SubmissionID}
		}
		entry.claimed = true
	}
	return entry.prompt, nil
}

// AcknowledgeRecalledPrompt drops attachment bytes only after delivery. Keep
// a tombstone so a late duplicate request cannot claim a different prompt.
func (b *Backend) AcknowledgeRecalledPrompt(workspaceID, sessionID, requestID string) {
	entry := b.recalledPrompt(workspaceID, sessionID, requestID)
	entry.mu.Lock()
	defer entry.mu.Unlock()
	if entry.claimed {
		entry.prompt = nil
	}
}

// Interrupt stops the active run for the session and lets the next queued
// prompt run.
func (b *Backend) Interrupt(workspaceID, sessionID string) error {
	o, err := b.ops(workspaceID)
	if err != nil {
		return err
	}
	o.Interrupt(sessionID)
	return nil
}

// QueuedPromptsList returns the list of queued prompt strings for a
// session.
func (b *Backend) QueuedPromptsList(workspaceID, sessionID string) ([]string, error) {
	o, err := b.ops(workspaceID)
	if err != nil {
		return nil, err
	}
	return o.QueuedPromptsList(sessionID), nil
}

// QueuedPromptSummaries describes the prompts queued for a session.
func (b *Backend) QueuedPromptSummaries(workspaceID, sessionID string) ([]message.QueuedPromptSummary, error) {
	o, err := b.ops(workspaceID)
	if err != nil {
		return nil, err
	}
	return o.QueuedPromptSummaries(sessionID), nil
}

// GetDefaultSmallModel returns the default small model for a provider.
func (b *Backend) GetDefaultSmallModel(workspaceID, providerID string) (config.SelectedModel, error) {
	o, err := b.ops(workspaceID)
	if err != nil {
		return config.SelectedModel{}, err
	}
	return o.GetDefaultSmallModel(providerID), nil
}

// RunShellCommand runs a shell command in the workspace directory and
// persists the command + output as a user message in the session.
func (b *Backend) RunShellCommand(ctx context.Context, workspaceID string, req proto.ShellCommandRequest) (proto.ShellCommandResponse, error) {
	return b.RunShellCommandLive(ctx, workspaceID, "", "", req)
}

// RunShellCommandLive is RunShellCommand with live output (engine-design.md
// 5.9 a): with clientID and requestID both set, the output also goes to
// that app as shell_output events while the command runs. The response
// stays authoritative; the events are best effort.
func (b *Backend) RunShellCommandLive(ctx context.Context, workspaceID, clientID, requestID string, req proto.ShellCommandRequest) (proto.ShellCommandResponse, error) {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return proto.ShellCommandResponse{}, err
	}
	cmd := ShellCommand{
		SessionID:      req.SessionID,
		Command:        req.Command,
		Cwd:            ws.Path,
		Env:            append(os.Environ(), ws.Env...),
		TermWidth:      req.TermWidth,
		IsFirstMessage: req.IsFirstMessage,
	}
	if clientID != "" && requestID != "" {
		out := newShellStream(requestID, req.SessionID, func(o proto.ShellOutput) {
			b.SendTo(workspaceID, clientID, EngineEvent{Type: pubsub.PayloadTypeShellOutput, Kind: pubsub.CreatedEvent, Payload: o})
		})
		defer out.close()
		cmd.OnProgress = out.write
	}
	return ws.ops().RunShellCommand(ctx, cmd)
}

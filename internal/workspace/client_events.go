package workspace

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/charmbracelet/crush/internal/agent/notify"
	"github.com/charmbracelet/crush/internal/agent/tools/mcp"
	"github.com/charmbracelet/crush/internal/app"
	"github.com/charmbracelet/crush/internal/client"
	"github.com/charmbracelet/crush/internal/herdr"
	"github.com/charmbracelet/crush/internal/history"
	"github.com/charmbracelet/crush/internal/log"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/permission"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/charmbracelet/crush/internal/question"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/skills"
	"github.com/charmbracelet/crush/internal/version"
)

func (w *ClientWorkspace) Subscribe(program *tea.Program) {
	defer log.RecoverPanic("ClientWorkspace.Subscribe", func() {
		slog.Info("TUI subscription panic: attempting graceful shutdown")
		program.Quit()
	})

	w.runSubscription(program.Send)
}

// maxRecoveryEscalate is the number of consecutive failed workspace
// recovery attempts after which the loop tells the UI the connection
// looks unrecoverable. It keeps retrying regardless: a hard stop would
// strand a user whose server comes back a minute later, and Shutdown can
// always cancel it.
const maxRecoveryEscalate = 20

// recoveryCreateTimeout bounds a single re-registration attempt. It is
// generous because workspace startup is slow (config, database, LSP, MCP);
// it exists only so an unresponsive server cannot pin the subscription
// goroutine indefinitely, and the loop simply retries when it trips.
// A var, not a const, so tests can shrink it.
var recoveryCreateTimeout = 30 * time.Second

// runSubscription subscribes to the workspace event stream and forwards
// translated events to send, reconnecting with capped exponential
// backoff whenever the stream drops. It returns only when the
// subscription context is cancelled (via Shutdown). Split out from
// Subscribe so it can be tested without a real *tea.Program.
//
// Two failures need more than a retry. A 404 means the server no longer
// knows this workspace, so resubscribing with the same ID can never
// succeed and the loop re-registers instead. And any stream that closes
// loses whatever was published while the client was away, so every
// re-established stream — even one that reconnects on the first try —
// re-asserts the client's session and asks the UI to resync.
func (w *ClientWorkspace) runSubscription(send func(tea.Msg)) {
	w.subStarted.Store(true)
	defer close(w.subDone)

	backoff := sseReconnectInitialBackoff
	degraded := false
	recoveryFailures := 0
	markDegraded := func(err error, stuck bool) {
		if degraded && !stuck {
			return
		}
		degraded = true
		send(ConnectionEvent{State: ConnectionDegraded, Err: err, Stuck: stuck})
	}

	for {
		if w.subCtx.Err() != nil {
			return
		}

		evc, err := w.client.SubscribeEvents(w.subCtx, w.workspaceID())
		if err != nil {
			if w.subCtx.Err() != nil {
				return
			}
			markDegraded(err, false)
			if !errors.Is(err, client.ErrNotFound) {
				slog.Error("Failed to subscribe to workspace events; retrying",
					"error", err, "retry_in", backoff)
			} else if w.recoverWorkspace() == nil {
				// Re-registered: resubscribe immediately under the fresh
				// workspace ID.
				backoff = sseReconnectInitialBackoff
				continue
			} else if w.subCtx.Err() == nil {
				recoveryFailures++
				if recoveryFailures == maxRecoveryEscalate {
					markDegraded(ErrWorkspaceGone, true)
				}
			}
			if !w.sleepOrDone(backoff) {
				return
			}
			backoff = min(backoff*2, sseReconnectMaxBackoff)
			continue
		}

		if degraded {
			degraded = false
			recoveryFailures = 0
			w.afterReconnect(send)
		}
		backoff = sseReconnectInitialBackoff
		w.consumeEvents(evc, send)

		// The event channel closed: the server restarted, the stream was
		// interrupted, or the workspace briefly went away. Reconnect
		// after a short delay instead of leaving the TUI permanently
		// orphaned, which is what surfaced as a stuck "coder agent is
		// offline".
		if w.subCtx.Err() != nil {
			return
		}
		markDegraded(ErrStreamClosed, false)
		slog.Warn("Workspace event stream closed; reconnecting", "retry_in", backoff)
		if !w.sleepOrDone(backoff) {
			return
		}
		backoff = min(backoff*2, sseReconnectMaxBackoff)
	}
}

// recoverWorkspace re-registers the workspace after the server reported it
// gone: it re-creates it from the cached snapshot (the server's own view of
// path, data dir, flags and env), adopts the new ID, and re-initializes the
// coder agent when the config is ready, mirroring the startup handshake. The
// server's path dedupe means this either rejoins a live sibling workspace or
// mints a fresh one. It must only be called from the subscription goroutine,
// the only writer of the cached ID.
//
// The create deliberately runs detached from the subscription context: the
// server does not abandon a create when the requesting connection goes away,
// so cancelling would hide the outcome while the workspace got registered
// anyway. Riding it out means the ID is known by the time Shutdown looks,
// and retirement covers a lost response regardless. Its own timeout keeps a
// wedged server from pinning the subscription goroutine forever; the client
// SDK sets no request timeout of its own.
func (w *ClientWorkspace) recoverWorkspace() error {
	ctx, cancel := context.WithTimeout(
		context.WithoutCancel(w.subCtx), recoveryCreateTimeout,
	)
	defer cancel()
	created, err := w.client.CreateWorkspace(ctx, w.recreateArgs())
	if err != nil {
		slog.Error("Failed to re-register workspace; retrying", "error", err)
		return err
	}
	if created.Config != nil {
		created.Config.SetupAgents()
		created.Config.NormalizeOptions()
	}
	w.mu.Lock()
	oldID := w.ws.ID
	w.ws = *created
	w.mu.Unlock()
	slog.Info("Re-registered workspace after server-side loss",
		"old_id", oldID, "new_id", created.ID)

	if created.Config != nil && created.Config.IsConfigured() {
		if err := w.InitCoderAgent(w.subCtx); err != nil {
			// Matches the startup handshake: agent init failure is
			// logged, not fatal, since the user can still pick a model.
			slog.Error("Failed to initialize coder agent after workspace recovery", "error", err)
		}
	}
	return nil
}

// recreateArgs derives the CreateWorkspace request used for recovery from
// the cached snapshot. The ID is dropped so the server can dedupe by
// path or mint a fresh workspace, and Version carries this client's
// version, matching the startup handshake.
func (w *ClientWorkspace) recreateArgs() proto.Workspace {
	ws := w.cached()
	return proto.Workspace{
		Path:     ws.Path,
		DataDir:  ws.DataDir,
		Debug:    ws.Debug,
		YOLO:     ws.YOLO,
		Channels: ws.Channels,
		Env:      ws.Env,
		Version:  version.Version,
	}
}

// afterReconnect runs once a degraded subscription is re-established. It
// re-asserts the client's current-session selection, since the server's
// presence entry (or the whole workspace) may have been re-created while we
// were away, and tells the UI to resync state published while detached. The
// SSE handler attaches the client before writing its 200, so the presence
// call cannot be rejected as not-attached here.
func (w *ClientWorkspace) afterReconnect(send func(tea.Msg)) {
	w.mu.RLock()
	sid := w.lastSession
	w.mu.RUnlock()
	if sid != "" {
		if err := w.SetCurrentSession(w.subCtx, sid); err != nil {
			slog.Warn("Failed to re-assert current session after reconnect", "error", err)
		}
	}
	send(ConnectionEvent{State: ConnectionRecovered})
}

// sleepOrDone waits for d or until the subscription context is
// cancelled. It reports false when the context was cancelled, signalling
// the caller to stop reconnecting.
func (w *ClientWorkspace) sleepOrDone(d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-w.subCtx.Done():
		return false
	}
}

// consumeEvents drives the workspace event loop. It is split out from
// Subscribe so tests can drive it without a real *tea.Program.
// ConfigChanged events trigger a workspace refresh; all other events
// are translated into domain types and forwarded to send.
func (w *ClientWorkspace) consumeEvents(evc <-chan any, send func(tea.Msg)) {
	for ev := range evc {
		// Forward events to herdr if running inside a herdr pane.
		if hev := herdr.Translate(ev); hev != nil {
			w.herdrClient.HandleEvent(hev)
		}

		if _, ok := ev.(pubsub.Event[proto.ConfigChanged]); ok {
			w.refreshWorkspace()
			continue
		}
		translated := w.translateEvent(ev)
		if translated != nil && send != nil {
			send(translated)
		}
	}
}

// shutdownDrainTimeout bounds how long Shutdown waits for the subscription
// loop to stop. Exceeding it is not a correctness problem — retiring the
// client releases whatever a late recovery registers — it only makes the
// goodbye less tidy.
const shutdownDrainTimeout = 5 * time.Second

func (w *ClientWorkspace) Shutdown() {
	// Stop the reconnect/recovery loop first, then wait for it: cancelling
	// alone does not unwind a workspace recovery that is already in
	// flight, and we want to release the workspace that recovery ended up
	// with rather than one it is about to replace.
	if w.subCancel != nil {
		w.subCancel()
	}
	w.awaitSubscription()
	w.herdrClient.Close()

	// Retiring the client releases every claim it holds, on every workspace,
	// and blocks any further create from this client ID. That is what makes
	// teardown exact even when a recovery create's response was lost: the
	// create either landed before this call, and its claim is released here,
	// or it arrives afterwards and registers nothing.
	err := w.client.RetireClient(context.Background())
	if err == nil {
		return
	}
	if !errors.Is(err, client.ErrUnsupported) {
		slog.Warn("Failed to retire client on the server", "error", err)
		return
	}
	// The server predates client retirement, so fall back to releasing
	// the workspace we know about. Nothing better is possible against an
	// older server.
	_ = w.client.DeleteWorkspace(context.Background(), w.workspaceID())
}

// awaitSubscription waits for the subscription loop to return. It returns
// immediately when the loop never started, which is the case for
// workspaces shut down before Subscribe runs.
func (w *ClientWorkspace) awaitSubscription() {
	if !w.subStarted.Load() || w.subDone == nil {
		return
	}
	t := time.NewTimer(shutdownDrainTimeout)
	defer t.Stop()
	select {
	case <-w.subDone:
	case <-t.C:
		slog.Warn("Timed out waiting for the workspace subscription to stop")
	}
}

// translateEvent converts proto-typed SSE events into the domain types
// that the TUI's Update() method expects. Skills events also update the
// process-local skills.Manager so callers reading
// skills.GetLatestStates see fresh data.
func (w *ClientWorkspace) translateEvent(ev any) tea.Msg {
	switch e := ev.(type) {
	case pubsub.Event[proto.LSPEvent]:
		return pubsub.Event[LSPEvent]{
			Type: e.Type,
			Payload: LSPEvent{
				Type:            LSPEventType(e.Payload.Type),
				Name:            e.Payload.Name,
				State:           e.Payload.State,
				Error:           e.Payload.Error,
				DiagnosticCount: e.Payload.DiagnosticCount,
			},
		}
	case pubsub.Event[proto.MCPEvent]:
		return pubsub.Event[mcp.Event]{Type: e.Type, Payload: proto.MCPEventToDomain(e.Payload)}
	case pubsub.Event[proto.PermissionRequest]:
		return pubsub.Event[permission.PermissionRequest]{
			Type:    e.Type,
			Payload: proto.PermissionRequestToDomain(e.Payload),
		}
	case pubsub.Event[proto.PermissionNotification]:
		return pubsub.Event[permission.PermissionNotification]{
			Type:    e.Type,
			Payload: proto.PermissionNotificationToDomain(e.Payload),
		}
	case pubsub.Event[proto.QuestionRequest]:
		return pubsub.Event[question.Request]{
			Type:    e.Type,
			Payload: proto.QuestionRequestToDomain(e.Payload),
		}
	case pubsub.Event[proto.QuestionNotification]:
		return pubsub.Event[question.Notification]{
			Type:    e.Type,
			Payload: question.Notification{BatchID: e.Payload.BatchID, SessionID: e.Payload.SessionID},
		}
	case pubsub.Event[proto.Message]:
		return pubsub.Event[message.Message]{
			Type:    e.Type,
			Payload: proto.MessageToDomain(e.Payload),
		}
	case pubsub.Event[proto.Session]:
		return pubsub.Event[session.Session]{
			Type:    e.Type,
			Payload: proto.SessionToDomain(e.Payload),
		}
	case pubsub.Event[proto.File]:
		return pubsub.Event[history.File]{
			Type:    e.Type,
			Payload: proto.FileToDomain(e.Payload),
		}
	case pubsub.Event[proto.AgentEvent]:
		return pubsub.Event[notify.Notification]{
			Type:    e.Type,
			Payload: proto.AgentEventToDomain(e.Payload),
		}
	case pubsub.Event[proto.RunComplete]:
		// Translate the wire-level proto.RunComplete back into the
		// agent's domain notify.RunComplete. Without this case the
		// default branch below warns on every run completion in the
		// server-mode TUI, even though the TUI itself doesn't act
		// on RunComplete — converting silently keeps the workspace
		// event bridge symmetric with the server-side wrapEvent.
		return pubsub.Event[notify.RunComplete]{
			Type:    e.Type,
			Payload: proto.RunCompleteToDomain(e.Payload),
		}
	case pubsub.Event[proto.SkillsEvent]:
		states := proto.SkillStatesToDomain(e.Payload.States)
		if w.skills != nil {
			w.skills.SetLatestStates(states)
		}
		return pubsub.Event[skills.Event]{
			Type:    e.Type,
			Payload: skills.Event{States: states},
		}
	case pubsub.Event[proto.UpdateAvailable]:
		return app.UpdateAvailableMsg{
			CurrentVersion: e.Payload.CurrentVersion,
			LatestVersion:  e.Payload.LatestVersion,
			IsDevelopment:  e.Payload.IsDevelopment,
		}
	default:
		slog.Warn("Unknown event type in translateEvent", "type", fmt.Sprintf("%T", ev))
		return nil
	}
}

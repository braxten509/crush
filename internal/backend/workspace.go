package backend

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/charmbracelet/crush/internal/app"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/skills"
	"github.com/charmbracelet/crush/internal/version"
)

// clientState tracks one client's claim on a workspace.
//
//   - streams counts the number of live SSE event streams the client
//     currently has open against the workspace.
//   - holdTimer is non-nil in the two timer-held states: the client
//     created the workspace but has not yet attached an SSE stream
//     (fires after createGrace), or the client's last SSE stream dropped
//     without an explicit release (fires after detachGrace, giving the
//     client's reconnect loop a window to re-attach). Either way the
//     timer releases the claim when it expires.
//   - currentSessionID records which session this client is currently
//     viewing. Empty string means the client has no session selected
//     (e.g. the landing screen). Cleared automatically when the
//     clientState entry is removed.
//   - sessionReported records whether the client has ever reported a
//     current session via [Backend.SetCurrentSession]. Clients that
//     never report one (e.g. API clients predating session reporting)
//     are indistinguishable from landing-screen clients by session ID
//     alone; this flag lets the SSE filter fail open for them.
//   - released marks that the client gave up its claim explicitly while
//     streams were still open, which a clean exit does. The final stream
//     detach then tears down immediately instead of waiting out the
//     detach grace for a client that is not coming back.
//
// streams and holdTimer are mutually exclusive in practice (the hold
// timer is stopped the moment an SSE stream attaches), but both being
// zero/nil means the entry has been released and should be removed.
type clientState struct {
	streams          int
	holdTimer        *time.Timer
	currentSessionID string
	sessionReported  bool
	released         bool
}

// Workspace represents a running [app.App] workspace with its
// associated resources and state.
type Workspace struct {
	*app.App
	ID     string
	Path   string
	Cfg    *config.ConfigStore
	Env    []string
	Skills *skills.Manager

	// resolvedPath is the path used as the dedup key in
	// Backend.pathIndex. It is filepath.EvalSymlinks(filepath.Abs(Path))
	// with fallback to the cleaned absolute path.
	resolvedPath string

	// ctx is the workspace-scoped run context. It is derived from
	// the backend context in CreateWorkspace and lives for the
	// lifetime of the workspace; cancel tears it down. Agent runs
	// dispatched on behalf of this workspace are bound to ctx so
	// their lifetime is owned by the workspace, not by any single
	// client's HTTP request.
	ctx    context.Context
	cancel context.CancelFunc

	// runMu guards closing and gates dispatch of new agent runs.
	// closing is set by Shutdown so no new runs are accepted once
	// teardown has begun. runWG tracks dispatched agent goroutines
	// so Shutdown can wait for them to return before app cleanup.
	runMu   sync.Mutex
	closing bool
	runWG   sync.WaitGroup

	// clientsMu guards clients. It is held only briefly (no IO).
	clientsMu sync.Mutex
	// clients tracks each client's claim on this workspace. Refcount
	// is a derived value: len(clients).
	clients map[string]*clientState

	// rootCache memoizes [Backend.RootSessionID] resolutions, keyed by
	// session ID. Session parentage is fixed at creation, so entries
	// never go stale; the cache keeps a DB read per event per client
	// off the SSE hot path.
	rootCache sync.Map // map[string]string

	// shutdownFn is the function invoked by [Backend.teardown] to
	// release the workspace's underlying resources. It defaults to the
	// embedded [app.App.Shutdown]; tests may override it to avoid
	// driving a full [app.App] through shutdown.
	shutdownFn func()

	// streamMu guards streams: the apps' SSE streams that take engine-sent
	// events ([Backend.SubscribeClient], [Backend.SendTo],
	// [Backend.SendAll]). The map is made by the first subscribe.
	streamMu sync.Mutex
	streams  map[*clientStream]struct{}

	// The build pieces' own state (engine-design.md, Build pieces): live
	// work (B), tree actions (C) and phone sharing (D).
	live  liveState
	tree  treeState
	share shareState
}

// invokeShutdown calls the workspace shutdown hook if set, falling
// back to the workspace [Workspace.Shutdown] wrapper when not.
func (w *Workspace) invokeShutdown() {
	if w.shutdownFn != nil {
		w.shutdownFn()
		return
	}
	if w.App != nil {
		w.Shutdown()
	}
}

// Shutdown tears the workspace down in an order that is safe for
// agent runs whose lifetime is bound to the workspace context. It
// shadows the promoted [app.App.Shutdown] so callers reaching
// ws.Shutdown() always observe this ordering:
//
//  1. Mark the workspace closing so no new agent runs are accepted.
//  2. Cancel the workspace run context so any dispatched goroutine
//     that has not yet registered its per-session cancel still
//     observes cancellation.
//  3. Cancel active coordinator work for runs that already
//     registered their per-session cancel function.
//  4. Wait for dispatched agent goroutines to return.
//  5. Run the embedded [app.App.Shutdown] cleanup (DB, LSP, etc).
//
// CancelAll is idempotent, so the second call inside app.App.Shutdown
// is harmless; the important guarantee is that cancel -> CancelAll ->
// runWG.Wait completes before the embedded cleanup touches the DB.
func (w *Workspace) Shutdown() {
	w.runMu.Lock()
	w.closing = true
	w.runMu.Unlock()

	if w.cancel != nil {
		w.cancel()
	}
	if w.App != nil && w.AgentCoordinator != nil {
		w.AgentCoordinator.CancelAll()
	}
	w.runWG.Wait()
	if w.App != nil {
		w.App.Shutdown()
	}
}

// shownSessions lists the sessions some client with a live stream shows
// (each with AttachedClientsForSession > 0).
func (w *Workspace) shownSessions() []string {
	w.clientsMu.Lock()
	defer w.clientsMu.Unlock()
	var shown []string
	for _, cs := range w.clients {
		if cs.streams > 0 && cs.currentSessionID != "" && !slices.Contains(shown, cs.currentSessionID) {
			shown = append(shown, cs.currentSessionID)
		}
	}
	return shown
}

// AttachedClientsForSession returns the number of clients in this
// workspace whose currentSessionID equals sessionID and which have at
// least one live SSE stream. Hold-only clients (streams == 0) do not
// contribute. Acquires the workspace's [clientsMu] briefly; the
// returned count is a point-in-time snapshot.
func (w *Workspace) AttachedClientsForSession(sessionID string) int {
	w.clientsMu.Lock()
	defer w.clientsMu.Unlock()
	n := 0
	for _, cs := range w.clients {
		if cs.streams > 0 && cs.currentSessionID == sessionID {
			n++
		}
	}
	return n
}

func workspaceToProto(ws *Workspace) proto.Workspace {
	cfg := ws.Cfg.Config()
	out := proto.Workspace{
		ID:       ws.ID,
		Path:     ws.Path,
		YOLO:     ws.Cfg.Overrides().SkipPermissionRequests,
		Channels: ws.Cfg.Overrides().EnabledChannels,
		DataDir:  cfg.Options.DataDirectory,
		Debug:    cfg.Options.Debug,
		Config:   cfg,
		Env:      ws.Env,
		Version:  version.Version,

		ConfigPath: ws.Cfg.GlobalDataPath(),
	}
	if ws.Skills != nil {
		out.Skills = proto.SkillStatesFromDomain(ws.Skills.States())
	}
	return out
}

// Package backend provides transport-agnostic operations for managing
// workspaces, sessions, agents, permissions, and events. It is consumed
// by protocol-specific layers such as HTTP (server) and ACP.
package backend

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"sync"
	"time"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/csync"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/version"
)

// Common errors returned by backend operations.
var (
	ErrWorkspaceNotFound       = errors.New("workspace not found")
	ErrLSPClientNotFound       = errors.New("LSP client not found")
	ErrAgentNotInitialized     = errors.New("agent coordinator not initialized")
	ErrAgentBusy               = errors.New("agent is busy with a run")
	ErrPathRequired            = errors.New("path is required")
	ErrInvalidPermissionAction = errors.New("invalid permission action")
	ErrUnknownCommand          = errors.New("unknown command")
	ErrInvalidClientID         = errors.New("invalid client_id")
	ErrClientNotAttached       = errors.New("client not attached")
	ErrWorkspaceClosing        = errors.New("workspace closing")
	ErrServerShuttingDown      = errors.New("server is shutting down")
	ErrServerNotIdle           = errors.New("server is hosting live workspaces")
	ErrClientRetired           = errors.New("client has been retired")
	ErrChannelOptInMismatch    = errors.New("requested channels differ from the existing workspace; channels are an explicit opt-in and are not shared across duplicate creates")
)

// DefaultCreateGrace is the window in which a client must open an SSE
// stream after creating a workspace before its creation hold is
// released. Exposed as a package variable so tests can shorten it.
var DefaultCreateGrace = 30 * time.Second

// DefaultIdleShutdownDelay is how long the server stays alive after its
// last workspace is released before it shuts itself down. The delay
// exists so a client that closes one session and opens another moments
// later (the same directory or a different one) reuses the still-running
// server instead of racing its shutdown: with an immediate shutdown the
// new client can attach to — or create a workspace on — a server that is
// already tearing down, and then observe its coder agent as "offline".
// Any workspace create within the window cancels the pending shutdown.
// Overridable via CRUSH_SERVER_IDLE_TIMEOUT (seconds; 0 restores the
// old shut-down-immediately behavior).
var DefaultIdleShutdownDelay = 60 * time.Second

// DefaultDetachGrace is how long a client's claim on a workspace survives
// after its last SSE stream drops without an explicit release. The stream
// is the client's refcount claim, so tearing the workspace down the instant
// it closes turns any momentary drop (a hiccup, a suspended laptop, a proxy
// timeout) into a permanently lost workspace: the client's reconnect comes
// back milliseconds later to an ID the server no longer knows. A client
// that released its claim first (a clean exit) skips the grace. Overridable
// via CRUSH_SERVER_DETACH_GRACE (seconds; 0 restores immediate teardown).
var DefaultDetachGrace = 10 * time.Second

// ShutdownFunc is called when the backend needs to trigger a server
// shutdown (e.g. when the last workspace is removed).
type ShutdownFunc func()

// Backend provides transport-agnostic business logic for the Crush
// server. It manages workspaces and delegates to [app.App] services.
//
// Locking order: when both [Backend.mu] and [Workspace.clientsMu] are
// held at once, [Backend.mu] is acquired first. Detach paths
// ([detachStream], [releaseHoldLocked], [expireHold]) only hold
// [Workspace.clientsMu] briefly, drop it, then call [teardown] which
// takes [Backend.mu] (and then re-takes [Workspace.clientsMu] to
// re-check that the workspace has not been re-claimed). This avoids
// the AB/BA hazard with [CreateWorkspace], which holds [Backend.mu]
// while calling [registerClient] so that a workspace cannot be torn
// down beneath it.
type Backend struct {
	workspaces *csync.Map[string, *Workspace]
	// pathIndex maps a resolved absolute workspace path to its
	// workspace ID. Reads and writes are serialised via mu so
	// concurrent CreateWorkspace calls at the same path deduplicate
	// deterministically.
	pathIndex map[string]string
	// pending counts CreateWorkspace calls that have committed to the
	// slow initialization path (config/db/app setup) but have not yet
	// registered their workspace in the map. It is guarded by mu.
	// teardown must observe pending == 0 in addition to an empty
	// workspace map before triggering server shutdown: otherwise a
	// teardown of the last live workspace could race ahead of a
	// concurrent create — which releases mu during its slow init — and
	// shut the whole server down out from under the workspace being
	// born.
	pending int
	// shutdownTimer, when non-nil, is an armed idle-shutdown timer
	// waiting out lingerDelay before it shuts the server down. It is
	// guarded by mu and cancelled the moment a new create arrives.
	shutdownTimer *time.Timer
	// closing latches the decision to exit. Every site that commits to
	// a shutdown sets it while still holding mu, because shutdownFn has
	// to run unlocked; CreateWorkspace refuses once it is set. That
	// makes the shutdown-vs-create decision atomic: a create can never
	// be handed a workspace on a process that is already leaving.
	closing bool
	// retired holds the IDs of clients that announced their exit via
	// RetireClient. Creates from a retired client are refused, which is what
	// lets a client release a workspace whose ID it never learned.
	//
	// Entries are never pruned, and deliberately so: an entry is exactly
	// what refuses a create that was already on the wire when the client
	// said goodbye, and HTTP gives no ordering between two requests, so
	// there is no moment at which the server can prove none is still coming.
	// Dropping an entry to save memory would reintroduce the orphaned
	// workspace this mechanism exists to prevent. The cost is one UUID per
	// client process, and the server only outlives its clients for as long
	// as sessions keep arriving inside the idle-shutdown window.
	retired  map[string]struct{}
	mu       sync.Mutex
	recallMu sync.Mutex
	// Unacknowledged recalls retain their payload across failed responses.
	recalledPrompts map[string]*retainedRecall

	cfg         *config.ConfigStore
	ctx         context.Context
	shutdownFn  ShutdownFunc
	createGrace time.Duration
	lingerDelay time.Duration
	detachGrace time.Duration

	// engineDataDir is the canonical data folder of the one project an
	// engine serves ([Backend.SetEngine]), or "" on the legacy server.
	// Guarded by mu.
	engineDataDir string
	// firstJoinGrace is how long an engine waits for its first app
	// ([Backend.ArmFirstJoinGrace]). Guarded by mu.
	firstJoinGrace time.Duration
	// startedAt is when the backend was created, for GET /v1/engine.
	startedAt time.Time

	// promptWatchersMu guards promptWatchers: the waiting-for-an-answer
	// watcher of each workspace whose agent started (pending_prompts.go).
	// It is keyed by the workspace and cleaned up by the teardown hook.
	promptWatchersMu sync.Mutex
	promptWatchers   map[*Workspace]*promptWatcher
}

// New creates a new [Backend]. The returned backend routes MCP channel
// events into hosted workspaces for its lifetime (until ctx is
// canceled); see [Backend.startChannelRouter].
func New(ctx context.Context, cfg *config.ConfigStore, shutdownFn ShutdownFunc) *Backend {
	b := &Backend{
		workspaces:  csync.NewMap[string, *Workspace](),
		pathIndex:   make(map[string]string),
		retired:     make(map[string]struct{}),
		cfg:         cfg,
		ctx:         ctx,
		shutdownFn:  shutdownFn,
		createGrace: DefaultCreateGrace,
		lingerDelay: idleShutdownDelayFromEnv(),
		detachGrace: durationFromEnv("CRUSH_SERVER_DETACH_GRACE", DefaultDetachGrace),
		startedAt:   time.Now(),

		promptWatchers: make(map[*Workspace]*promptWatcher),
	}
	b.startChannelRouter()
	return b
}

// idleShutdownDelayFromEnv returns the idle-shutdown delay, honoring a
// CRUSH_SERVER_IDLE_TIMEOUT override (in seconds; 0 disables lingering).
func idleShutdownDelayFromEnv() time.Duration {
	return durationFromEnv("CRUSH_SERVER_IDLE_TIMEOUT", DefaultIdleShutdownDelay)
}

// durationFromEnv reads a whole number of seconds from the named
// environment variable, falling back to def when it is unset or
// unparseable. Zero is a meaningful value for both lifecycle windows it
// configures, so it is accepted.
func durationFromEnv(name string, def time.Duration) time.Duration {
	if v := os.Getenv(name); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
			return time.Duration(secs) * time.Second
		}
	}
	return def
}

// SetCreateGrace overrides the create-grace window. Intended for tests
// that need short timeouts.
func (b *Backend) SetCreateGrace(d time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.createGrace = d
}

// SetDetachGrace overrides how long a client's claim survives after its
// last SSE stream drops. A value <= 0 restores the tear-down-immediately
// behavior. Intended for tests.
func (b *Backend) SetDetachGrace(d time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.detachGrace = d
}

// SetIdleShutdownDelay overrides how long the server lingers after its
// last workspace is released before shutting down. A value <= 0 restores
// the shut-down-immediately behavior. Intended for tests.
func (b *Backend) SetIdleShutdownDelay(d time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lingerDelay = d
}

// GetWorkspace retrieves a workspace by ID.
func (b *Backend) GetWorkspace(id string) (*Workspace, error) {
	ws, ok := b.workspaces.Get(id)
	if !ok {
		return nil, ErrWorkspaceNotFound
	}
	return ws, nil
}

// ListWorkspaces returns all running workspaces.
func (b *Backend) ListWorkspaces() []proto.Workspace {
	workspaces := []proto.Workspace{}
	for _, ws := range b.workspaces.Seq2() {
		workspaces = append(workspaces, workspaceToProto(ws))
	}
	return workspaces
}

// GetWorkspaceProto returns the proto representation of a workspace.
func (b *Backend) GetWorkspaceProto(id string) (proto.Workspace, error) {
	ws, err := b.GetWorkspace(id)
	if err != nil {
		return proto.Workspace{}, err
	}
	return workspaceToProto(ws), nil
}

// VersionInfo returns server version information.
func (b *Backend) VersionInfo() proto.VersionInfo {
	return proto.VersionInfo{
		Version:   version.Version,
		Commit:    version.Commit,
		BuildID:   version.BuildID,
		GoVersion: runtime.Version(),
		Platform:  fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH),
		Protocol:  proto.ProtocolVersion,
	}
}

// Config returns the server-level configuration.
func (b *Backend) Config() *config.ConfigStore {
	return b.cfg
}

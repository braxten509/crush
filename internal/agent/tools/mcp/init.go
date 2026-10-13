// Package mcp provides functionality for managing Model Context Protocol (MCP)
// clients within the Crush application.
package mcp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/csync"
	mcpoauth "github.com/charmbracelet/crush/internal/oauth/mcp"
	"github.com/charmbracelet/crush/internal/permission"
	"github.com/charmbracelet/crush/internal/pubsub"
)

// parseLevel converts an MCP logging level string to a slog.Level. The
// entire MCP logging feature is deprecated per SEP-2577 but remains
// functional; servers may still send log notifications during the
// deprecation window.
func parseLevel(level string) slog.Level {
	switch level {
	case "info":
		return slog.LevelInfo
	case "notice":
		return slog.LevelInfo
	case "warning":
		return slog.LevelWarn
	default:
		return slog.LevelDebug
	}
}

var (
	sessions = csync.NewMap[string, *ClientSession]()
	states   = csync.NewMap[string, ClientInfo]()
	authURLs = csync.NewMap[string, *mcpoauth.Handler]()
	broker   = pubsub.NewBroker[Event]()
	initOnce sync.Once
	initDone = make(chan struct{})

	// initStarted records whether Initialize has been armed. WaitForInit only
	// blocks once initialization is expected; coordinators built outside app
	// startup never arm it and so must not wait forever. initArmedAt anchors
	// WaitForInitBudget's deadline: the budget is measured from arming, not
	// from each call, so sequential waiters share one deadline instead of
	// stacking fresh budgets.
	initMu      sync.Mutex
	initStarted bool
	initArmedAt time.Time

	// renewMus serializes lazy session renewals per server so concurrent tool
	// calls cannot race to rebuild the same session.
	renewMusMu sync.Mutex
	renewMus   = map[string]*sync.Mutex{}

	// gens hands out a per-server generation number. teardown bumps a
	// server's generation; an init goroutine captures it at launch and only
	// commits its session if the generation is still current. A config
	// change that restarts a server mid-connect thus invalidates the
	// in-flight attempt instead of letting it register a stale session.
	gens = csync.NewMap[string, uint64]()

	// suppressMus serializes browser-suppression per server so only one
	// remote (server-driven) OAuth flow is active for a server at a time.
	suppressMus = csync.NewMap[string, *sync.Mutex]()

	// newSession creates a client session. It is a seam so tests can exercise
	// renewal concurrency without spawning a real transport.
	newSession = createSession
)

// suppressBrowserKey marks a context as requesting the OAuth handler not
// open a local browser; the caller surfaces the authorization URL itself.
type suppressBrowserKey struct{}

// ArmInit marks that MCP initialization is expected so WaitForInit blocks
// until it completes. Call this synchronously before launching Initialize in a
// goroutine; otherwise WaitForInit could observe the not-yet-started state and
// return early, letting the tool list be read before MCP tools register.
func ArmInit() {
	initMu.Lock()
	initStarted = true
	initArmedAt = time.Now()
	initMu.Unlock()
}

// DisarmInit undoes ArmInit so WaitForInit stops blocking and returns
// immediately. It exists for tests in other packages that arm the gate
// without ever running Initialize and must not leak a permanently-blocking
// gate into the rest of the test binary. Production code never needs it.
func DisarmInit() {
	initMu.Lock()
	initStarted = false
	initMu.Unlock()
}

// renewLock returns the per-server mutex used to serialize session renewals,
// creating it on first use.
func renewLock(name string) *sync.Mutex {
	renewMusMu.Lock()
	defer renewMusMu.Unlock()
	mu, ok := renewMus[name]
	if !ok {
		mu = &sync.Mutex{}
		renewMus[name] = mu
	}
	return mu
}

// Close closes all MCP clients. This should be called during application shutdown.
func Close(ctx context.Context) error {
	var wg sync.WaitGroup
	for name, session := range sessions.Seq2() {
		wg.Go(func() {
			done := make(chan error, 1)
			go func() {
				done <- session.Close()
			}()
			select {
			case err := <-done:
				if err != nil &&
					!errors.Is(err, io.EOF) &&
					!errors.Is(err, context.Canceled) &&
					err.Error() != "signal: killed" {
					slog.Warn("Failed to shutdown MCP client", "name", name, "error", err)
				}
			case <-ctx.Done():
			}
		})
	}
	wg.Wait()
	// Clean up any remaining OAuth handlers.
	for _, h := range authURLs.Seq2() {
		h.Close()
	}
	broker.Shutdown()
	return nil
}

// Initialize initializes MCP clients based on the provided configuration.
// forceStart lists config-disabled servers with a repository-scoped enabled
// override; they are started even though their config entry is disabled.
// localDisabled lists servers with a repository-scoped disabled override;
// they are not started.
func Initialize(ctx context.Context, permissions permission.Service, cfg *config.ConfigStore, forceStart, localDisabled []string) {
	ArmInit()
	slog.Info("Initializing MCP clients")
	start := time.Now()

	var wg sync.WaitGroup
	// Initialize states for all configured MCPs
	for name, m := range cfg.Config().MCP {
		if slices.Contains(localDisabled, name) {
			updateState(name, StateDisabled, nil, nil, Counts{})
			slog.Debug("Skipping MCP disabled for this repository", "name", name)
			continue
		}
		if m.Disabled {
			if !slices.Contains(forceStart, name) {
				updateState(name, StateDisabled, nil, nil, Counts{})
				slog.Debug("Skipping disabled MCP", "name", name)
				continue
			}
			slog.Info("Starting config-disabled MCP with repository enabled override", "name", name)
		}

		// Set initial starting state
		wg.Add(1)
		goInitClient(ctx, cfg, name, m, &wg)
	}
	wg.Wait()
	initOnce.Do(func() { close(initDone) })
	// Non-interactive runs wait for this to finish before sending a prompt, so
	// the total is the floor on their startup latency. Interactive runs do not
	// wait, but the total still explains when late-arriving tools show up.
	slog.Debug("Finished initializing MCP clients", "duration", time.Since(start).Truncate(time.Millisecond).String())
}

// WaitForInit blocks until MCP initialization is complete, i.e. until
// Initialize has finished and closed initDone. If initialization was never
// armed (ArmInit was not called, e.g. a coordinator built outside app
// startup), there is nothing to wait for and this returns nil immediately
// rather than blocking until ctx is cancelled.
func WaitForInit(ctx context.Context) error {
	initMu.Lock()
	started := initStarted
	initMu.Unlock()
	if !started {
		return nil
	}
	select {
	case <-initDone:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// InitWaitBudget bounds how long a message turn waits for MCP
// initialization before proceeding without the servers that have not
// finished. It is generously above a healthy stdio server's startup
// (uv/npx take a few seconds) but far below the per-server handshake
// timeouts (15s-120s): a server that wedges mid-handshake — e.g. one
// that never answers the SEP-2575 server/discover probe — must not
// make the whole app look dead while its timeout runs out.
const InitWaitBudget = 10 * time.Second

// WaitForInitBudget blocks like WaitForInit, but only until the budget —
// measured from when initialization was armed, not from this call —
// elapses. Anchoring the deadline at arming means the several sequential
// waiters on a turn's path (readyWg's tool build, then the turn itself)
// share one deadline instead of each stacking a fresh budget while a
// server is wedged, and turns arriving after the deadline don't wait at
// all. It returns nil both when initialization completed and when the
// budget elapsed first — in the latter case the caller proceeds with
// whatever servers have registered so far, and stragglers appear on a
// later turn once they finish. The caller's own context ending is still
// reported as an error.
func WaitForInitBudget(ctx context.Context, budget time.Duration) error {
	initMu.Lock()
	started := initStarted
	armedAt := initArmedAt
	initMu.Unlock()
	if !started {
		return nil
	}
	waitCtx, cancel := context.WithDeadline(ctx, armedAt.Add(budget))
	defer cancel()
	if err := WaitForInit(waitCtx); err == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	slog.Warn("MCP initialization still pending after wait budget; continuing without unfinished servers", "budget", budget)
	return nil
}

// InitializeSingle initializes a single MCP client by name.
func InitializeSingle(ctx context.Context, name string, cfg *config.ConfigStore) error {
	m, exists := cfg.Config().MCP[name]
	if !exists {
		return fmt.Errorf("mcp '%s' not found in configuration", name)
	}

	if m.Disabled {
		updateState(name, StateDisabled, nil, nil, Counts{})
		slog.Debug("Skipping disabled MCP", "name", name)
		return nil
	}

	return initClient(ctx, cfg, name, m, currentGen(name), cfg.Resolver())
}

// InitializeSingleForced starts the named MCP client even when its config
// entry is disabled. Used by the repository-scoped "Toggle MCPs" override.
// The startup counterpart is the forceStart argument of Initialize, which
// replays the persisted enabled overrides. Config files are not touched.
func InitializeSingleForced(ctx context.Context, name string, cfg *config.ConfigStore) error {
	m, exists := cfg.Config().MCP[name]
	if !exists {
		return fmt.Errorf("mcp '%s' not found in configuration", name)
	}

	return initClient(ctx, cfg, name, m, currentGen(name), cfg.Resolver())
}

// persistOAuthToken saves the OAuth token from a session to the global
// config so it survives restarts.

// SetConfigDisabled persists the disabled flag of a single MCP server in
// the given config scope and applies the change to the running client:
// disabling tears the connection down, enabling starts it even if it was
// disabled before. A repository-scoped override takes precedence, so when
// localOverride is set only the config is written.
func SetConfigDisabled(ctx context.Context, cfg *config.ConfigStore, scope config.Scope, name string, disabled, localOverride bool) error {
	if err := cfg.SetMCPServerDisabledConfig(scope, name, disabled); err != nil {
		return err
	}
	if localOverride {
		return nil
	}
	return SetLocalDisabled(ctx, cfg, name, disabled)
}

// SetLocalDisabled applies a repository-scoped toggle to the running
// client: disabling tears the connection down, enabling starts it even if
// its config entry is disabled. Persisting the override is the caller's job.
func SetLocalDisabled(ctx context.Context, cfg *config.ConfigStore, name string, disabled bool) error {
	if disabled {
		return DisableSingle(cfg, name)
	}
	return InitializeSingleForced(ctx, name, cfg)
}

// DisableSingle disables and closes a single MCP client by name.
func DisableSingle(cfg *config.ConfigStore, name string) error {
	// teardown bumps the generation, invalidating any in-flight connect, and
	// the StateDisabled transition clears the recorded config so a later
	// re-enable (even with an unchanged config) is seen as new and restarts.
	teardown(name)
	updateState(name, StateDisabled, nil, nil, Counts{})
	slog.Info("Disabled mcp client", "name", name)
	return nil
}

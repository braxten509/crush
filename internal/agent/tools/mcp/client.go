package mcp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
)

// initClient initializes a single MCP client with the given configuration.
// gen is the server generation captured when the attempt was launched; the
// resulting session is only committed if the generation is still current, so
// a config change that restarts the server mid-connect discards this attempt.
func initClient(ctx context.Context, cfg *config.ConfigStore, name string, m config.MCPConfig, gen uint64, resolver config.VariableResolver) error {
	// OAuth MCPs without a usable cached token require user interaction
	// (browser auth). If a cached token exists with an access token
	// (even if expired), try connecting first so the SDK can attempt a
	// silent refresh. Only defer to the UI if no token is available at
	// all or the token is structurally invalid (empty access token).
	if m.OAuth && m.Type == config.MCPHttp && !hasUsableToken(m.OAuthToken) {
		if m.OAuthToken != nil {
			clearOAuthToken(cfg, name)
		}
		updateState(name, StateNeedsAuth, nil, nil, Counts{})
		clearMCPData(name)
		slog.Info("MCP server requires OAuth authentication", "name", name)
		return nil
	}

	updateState(name, StateStarting, nil, nil, Counts{}, withPending(m))
	_, err := connectAndRegister(ctx, cfg, name, m, gen, resolver, ChannelOptIn(m, cfg.Overrides().EnabledChannels, name))
	if err != nil {
		// If an OAuth MCP fails because the saved token is no longer
		// valid (e.g. refresh token expired or revoked) or no token
		// could be obtained, clear the stale token and prompt the user
		// to re-authenticate instead of leaving the server stuck in
		// StateError.
		if m.OAuth && m.Type == config.MCPHttp && isOAuthInitErr(err) {
			if m.OAuthToken != nil {
				clearOAuthToken(cfg, name)
			}
			updateState(name, StateNeedsAuth, nil, nil, Counts{})
			slog.Info("MCP OAuth token is no longer valid, re-authentication required", "name", name, "error", err)
			return nil
		}
		return err
	}
	return nil
}

// connectAndRegister creates a session, lists tools and prompts,
// registers them in global state, and transitions to StateConnected.
// Returns the session so callers can perform post-processing (e.g.
// token persistence).
//
// gen is the generation captured when this attempt was launched. If the
// server was torn down since (generation bumped), the freshly built session
// is closed and discarded instead of being registered over whatever the
// newer attempt is doing. This is what makes a config change that lands
// mid-connect converge on the latest config rather than a stale one.
func connectAndRegister(ctx context.Context, cfg *config.ConfigStore, name string, m config.MCPConfig, gen uint64, resolver config.VariableResolver, channelOptIn bool) (*ClientSession, error) {
	session, err := createSession(ctx, cfg, name, m, resolver, channelOptIn)
	if err != nil {
		return nil, err
	}

	// A teardown ran while we were connecting: a newer attempt owns this
	// server now. Bail before writing to any shared registry so we don't
	// clobber the newer attempt's registrations; just drop our own session.
	if currentGen(name) != gen {
		slog.Debug("Discarding stale MCP session after config change", "name", name)
		closeSession(name, session)
		return nil, context.Canceled
	}

	toolCount, err := registerSessionTools(ctx, cfg, name, session)
	if err != nil {
		slog.Error("Error listing tools", "error", err)
		updateState(name, StateError, err, nil, Counts{})
		closeSession(name, session)
		return nil, err
	}

	prompts, err := getPrompts(ctx, session)
	if err != nil {
		slog.Error("Error listing prompts", "error", err)
		updateState(name, StateError, err, nil, Counts{})
		closeSession(name, session)
		return nil, err
	}

	// Re-check before publishing: if a teardown landed during registration a
	// newer attempt owns the registries now, so leave them and our session
	// alone rather than overwriting its state.
	if currentGen(name) != gen {
		slog.Debug("Discarding stale MCP session after config change", "name", name)
		closeSession(name, session)
		return nil, context.Canceled
	}

	updatePrompts(name, prompts)
	sessions.Set(name, session)

	updateState(name, StateConnected, nil, session, Counts{
		Tools:   toolCount,
		Prompts: len(prompts),
	}, withConfig(m))

	return session, nil
}

// goInitClient launches initClient in a goroutine with panic recovery.
// Shared by Initialize and Reinitialize so the panic-to-state policy
// lives in one place. wg, if non-nil, is Done when the attempt finishes
// (success or failure); Initialize uses it to await startup. The goroutine
// captures the server's generation at launch so a concurrent teardown
// invalidates its result rather than letting it register a stale session.
func goInitClient(ctx context.Context, cfg *config.ConfigStore, name string, m config.MCPConfig, wg *sync.WaitGroup) {
	gen := currentGen(name)
	go func() {
		if wg != nil {
			defer wg.Done()
		}
		defer func() {
			if r := recover(); r != nil {
				var err error
				switch v := r.(type) {
				case error:
					err = v
				case string:
					err = fmt.Errorf("panic: %s", v)
				default:
					err = fmt.Errorf("panic: %v", v)
				}
				updateState(name, StateError, err, nil, Counts{})
				slog.Error("Panic in MCP client initialization", "error", err, "name", name)
			}
		}()
		start := time.Now()
		err := initClient(ctx, cfg, name, m, gen, cfg.Resolver())
		slog.Debug(
			"MCP client initialization finished",
			"name", name,
			"duration", time.Since(start).Truncate(time.Millisecond).String(),
			"error", err,
		)
	}()
}

// currentGen returns a server's current generation without bumping it.
func currentGen(name string) uint64 {
	g, _ := gens.Get(name)
	return g
}

// teardown closes a server's session and clears its tools, prompts,
// resources, and auth state, then bumps the server's generation so any
// in-flight initialization for it is discarded on commit. It leaves the
// states entry intact; callers decide whether to delete or update it.
// Shared by DisableSingle, removeServer, and the restart path in
// Reinitialize.
func teardown(name string) {
	g, _ := gens.Get(name)
	gens.Set(name, g+1)
	if session, ok := sessions.Take(name); ok {
		closeSession(name, session)
	}
	clearMCPData(name)
}

func getOrRenewClient(ctx context.Context, cfg *config.ConfigStore, name string) (*ClientSession, error) {
	m := cfg.Config().MCP[name]
	timeout := mcpTimeout(m)

	// Fast path: reuse a healthy session without taking the renewal lock.
	if sess, ok := sessions.Get(name); ok {
		if err := pingSession(ctx, sess, timeout); err == nil {
			return sess, nil
		}
	}

	// Serialize renewals per server. Two concurrent tool calls can both
	// observe a dead session and race to rebuild it: one may close the
	// session the other just registered, or overwrite and leak a live
	// replacement. Under this lock only the first arrival rebuilds; later
	// arrivals re-check and reuse the healthy result.
	mu := renewLock(name)
	mu.Lock()
	defer mu.Unlock()

	// Under the lock the map is stable: any in-flight renewal has finished and
	// either re-registered its session or failed and left none. A renewal
	// removes the session transiently (StateError takes it before rebuilding),
	// so this check must happen here rather than before the lock — otherwise a
	// caller arriving mid-renewal sees no session and wrongly reports the
	// server unavailable.
	sess, ok := sessions.Get(name)
	if !ok {
		return nil, fmt.Errorf("mcp '%s' not available", name)
	}

	// A concurrent goroutine may have already renewed the session while we
	// waited for the lock. Reuse it if it is now healthy.
	pingErr := pingSession(ctx, sess, timeout)
	if pingErr == nil {
		return sess, nil
	}

	state, _ := states.Get(name)
	// StateError closes the dead session and clears its tools, prompts, and
	// resources from the registry. Report the failure against the exact
	// session that failed the ping so only that session is torn down.
	updateState(name, StateError, maybeTimeoutErr(pingErr, timeout), sess, state.Counts)

	// Capture the generation so a reconcile teardown that lands mid-renewal
	// invalidates this rebuild instead of letting it clobber the newer one.
	gen := currentGen(name)
	newSess, err := newSession(ctx, cfg, name, m, cfg.Resolver(), ChannelOptIn(m, cfg.Overrides().EnabledChannels, name))
	if err != nil {
		clearMCPData(name)
		// If an OAuth MCP fails to reconnect because the token is no
		// longer valid, clear the stale token and prompt the user to
		// re-authenticate instead of leaving it in an error state.
		if m.OAuth && m.Type == config.MCPHttp {
			if m.OAuthToken != nil && isOAuthInitErr(err) {
				clearOAuthToken(cfg, name)
			}
			updateState(name, StateNeedsAuth, nil, nil, Counts{})
			slog.Info("MCP OAuth session expired, re-authentication required", "name", name, "error", err)
		}
		return nil, err
	}

	// A reconcile teardown ran while we were rebuilding: a newer attempt owns
	// this server now. Bail before writing to any shared registry so we don't
	// clobber the newer attempt's registrations; just drop our own session.
	if currentGen(name) != gen {
		closeSession(name, newSess)
		return nil, context.Canceled
	}

	// StateError cleared this server's tools, prompts, and resources from the
	// registry. Re-list and re-register them all on the fresh session and
	// recompute the counts from what actually registered; otherwise the agent
	// reconnects but the registries stay empty (the next tool call fails with
	// "tool not found") while the reported counts still advertise capabilities
	// that are no longer there.
	var counts Counts
	counts.Tools, err = registerSessionTools(ctx, cfg, name, newSess)
	if err != nil {
		updateState(name, StateError, err, nil, Counts{})
		closeSession(name, newSess)
		return nil, err
	}

	prompts, err := getPrompts(ctx, newSess)
	if err != nil {
		updateState(name, StateError, err, nil, Counts{})
		closeSession(name, newSess)
		return nil, err
	}
	updatePrompts(name, prompts)
	counts.Prompts = len(prompts)

	resources, err := getResources(ctx, newSess)
	if err != nil {
		updateState(name, StateError, err, nil, Counts{})
		closeSession(name, newSess)
		return nil, err
	}
	counts.Resources = updateResources(name, resources)

	// Re-check before publishing: if a teardown landed during registration a
	// newer attempt owns the registries now, so leave them and our session
	// alone rather than overwriting its state.
	if currentGen(name) != gen {
		closeSession(name, newSess)
		return nil, context.Canceled
	}

	sessions.Set(name, newSess)
	updateState(name, StateConnected, nil, newSess, counts, withConfig(m))
	return newSess, nil
}

// pingSession pings a session with the server's configured timeout.
func pingSession(ctx context.Context, s *ClientSession, timeout time.Duration) error {
	pingCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	err := s.Ping(pingCtx, nil)
	// MethodNotFound means the server answered; it just doesn't implement ping.
	var wireErr *jsonrpc.Error
	if errors.As(err, &wireErr) && wireErr.Code == jsonrpc.CodeMethodNotFound {
		return nil
	}
	return err
}

// closeSession closes an MCP session, logging only unexpected errors. EOF,
// context cancellation, and a killed child are the ordinary result of tearing
// a session down and are not worth surfacing.
func closeSession(name string, s *ClientSession) {
	if err := s.Close(); err != nil &&
		!errors.Is(err, io.EOF) &&
		!errors.Is(err, context.Canceled) &&
		err.Error() != "signal: killed" {
		slog.Warn("Error closing MCP session", "name", name, "error", err)
	}
}

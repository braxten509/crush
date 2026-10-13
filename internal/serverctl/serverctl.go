// Package serverctl starts, health-checks and replaces the detached Crush
// server that client/server mode talks to.
package serverctl

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"time"

	"github.com/charmbracelet/crush/internal/client"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/lock"
	crushlog "github.com/charmbracelet/crush/internal/log"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/server"
	"github.com/charmbracelet/crush/internal/version"
)

// LogFile returns the per-host log file shared by the server and by
// clients while they bring it up.
func LogFile(hostURL *url.URL) string {
	return filepath.Join(config.GlobalCacheDir(), "server-"+safeHostName(hostURL), "crush.log")
}

// maxStaleServerRetries bounds how many times workspace creation may be
// retried against a replacement server. Only one client can lose the race
// against a given server's shutdown, so a single retry is normally enough;
// the bound just keeps a pathological loop finite.
const maxStaleServerRetries = 3

// CreateWorkspace creates the workspace, retrying against a
// replacement when the server it reached has already committed to shutting
// itself down for being idle.
//
// That race is unavoidable: the server decides to exit while no client is
// talking to it, and a client can arrive between that decision and the
// socket going away. The decision is final on the server's side, so the
// only correct response is to bring up a fresh server and ask again
// instead of failing the command.
func CreateWorkspace(
	ctx context.Context, c *client.Client, req proto.Workspace, replace func() error,
) (*proto.Workspace, error) {
	for attempt := range maxStaleServerRetries {
		ws, err := c.CreateWorkspace(ctx, req)
		if err == nil {
			return ws, nil
		}
		if !errors.Is(err, client.ErrServerShuttingDown) || attempt == maxStaleServerRetries-1 {
			return nil, fmt.Errorf("failed to create workspace: %v", err)
		}
		slog.Warn("Server is shutting down; retrying against a replacement",
			"attempt", attempt+1, "error", err)
		if err := replace(); err != nil {
			return nil, err
		}
	}
	return nil, fmt.Errorf("failed to create workspace: server kept shutting down")
}

// ReplaceExiting waits out the socket of a server that has committed
// to exiting, then brings up a fresh one.
func ReplaceExiting(ctx context.Context, hostURL *url.URL, hostArg string) error {
	if hostURL.Scheme == "unix" {
		if err := awaitSocketGone(ctx, hostURL); err != nil {
			return err
		}
	}
	if err := spawnAndWaitReady(ctx, hostURL, hostArg); err != nil {
		return fmt.Errorf("failed to initialize crush server: %v", err)
	}
	return nil
}

// Ensure auto-starts a detached server if the socket file does not
// exist. When the socket exists, it verifies that the running server
// version matches the client; on mismatch it shuts down the old server
// and starts a fresh one.
func Ensure(ctx context.Context, hostURL *url.URL, hostArg string, debug bool) error {
	// Initialize the persistent log here so stale-socket diagnostics
	// emitted before connectToServer runs are captured in the per-host
	// server log file. crushlog.Setup uses sync.Once internally, so the
	// later call from connectToServer becomes a no-op.
	crushlog.Setup(LogFile(hostURL), debug)

	switch hostURL.Scheme {
	case "unix", "npipe":
		needsStart := false
		_, statErr := os.Stat(hostURL.Host)
		switch {
		case statErr == nil:
			// Probe the socket explicitly before the version-check
			// path. A stale unix socket file (the previous server
			// exited without cleaning up) would otherwise make
			// restartIfStale spin on a non-responsive endpoint; here
			// we detect it with a short DialTimeout and remove the
			// orphaned file so the normal spawn path can run.
			if hostURL.Scheme == "unix" {
				conn, dialErr := net.DialTimeout( //nolint:noctx
					hostURL.Scheme, hostURL.Host, 200*time.Millisecond,
				)
				if dialErr == nil {
					conn.Close()
				} else if server.IsStaleSocketErr(dialErr) {
					slog.Warn("Stale socket detected, removing",
						"path", hostURL.Host, "error", dialErr)
					if err := os.Remove(hostURL.Host); err != nil && !errors.Is(err, fs.ErrNotExist) {
						return fmt.Errorf("failed to remove stale server socket %q: %v", hostURL.Host, err)
					}
					needsStart = true
					break
				}
			}
			restarted, err := restartIfStale(ctx, hostURL)
			if err != nil {
				slog.Warn("Failed to check server version", "error", err)
			}
			needsStart = restarted || err != nil
		case errors.Is(statErr, fs.ErrNotExist):
			needsStart = true
		default:
			slog.Warn("Unexpected error stat'ing server socket, attempting cleanup",
				"path", hostURL.Host, "error", statErr)
			if err := os.Remove(hostURL.Host); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return fmt.Errorf("failed to remove stale server socket %q: %v", hostURL.Host, err)
			}
			needsStart = true
		}

		if needsStart {
			if err := spawnAndWaitReady(ctx, hostURL, hostArg); err != nil {
				return fmt.Errorf("failed to initialize crush server: %v", err)
			}
			return nil
		}

		if err := waitForServerReady(ctx, hostURL); err != nil {
			return fmt.Errorf("failed to initialize crush server: %v", err)
		}
	}

	return nil
}

// spawnAndWaitReady serializes the spawn-and-wait-for-readiness sequence
// across concurrent clients via an exclusive flock on
// $XDG_CACHE_HOME/crush/server-<safeHost>/start.lock.
//
// After acquiring the lock it re-probes readiness so that a client that
// blocked while another client was spawning can skip its own spawn and
// just use the now-running server. The lock is held only for the
// duration of "spawn + readiness probe" and released before the caller
// resumes its normal lifetime.
func spawnAndWaitReady(ctx context.Context, hostURL *url.URL, hostArg string) error {
	chDir, err := perHostServerDir(hostURL)
	if err != nil {
		return err
	}
	release, err := lock.File(ctx, filepath.Join(chDir, "start.lock"))
	if err != nil {
		// If the lock itself is unavailable, fall back to the
		// unsynchronized path rather than blocking the user.
		slog.Warn("Failed to acquire spawn lock, proceeding without single-flight", "error", err)
		if err := startDetachedServer(hostURL, hostArg); err != nil {
			return err
		}
		return waitForServerReady(ctx, hostURL)
	}
	defer release()

	// Another client may have just finished spawning while we were
	// waiting on the lock; if the server is already responsive, skip
	// the spawn entirely.
	probeCtx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	probeErr := quickHealthProbe(probeCtx, hostURL)
	cancel()
	if probeErr == nil {
		return nil
	}

	if err := startDetachedServer(hostURL, hostArg); err != nil {
		return err
	}
	return waitForServerReady(ctx, hostURL)
}

// quickHealthProbe issues a single readiness request with the caller's
// context and returns nil iff the server is responsive right now.
func quickHealthProbe(ctx context.Context, hostURL *url.URL) error {
	httpClient, reqURL, err := readinessHTTPClient(hostURL)
	if err != nil {
		return err
	}
	return probeHealth(ctx, httpClient, reqURL, hostURL)
}

// perHostServerDir returns (and creates) the cache directory used for
// per-host server state (logs, start.lock, etc.). The path is derived
// from the parsed host URL rather than the global flag so the same key
// is computed regardless of where the host came from.
func perHostServerDir(hostURL *url.URL) (string, error) {
	chDir := filepath.Join(config.GlobalCacheDir(), "server-"+safeHostName(hostURL))
	if err := os.MkdirAll(chDir, 0o700); err != nil {
		return "", fmt.Errorf("failed to create server working directory: %v", err)
	}
	return chDir, nil
}

// safeHostName returns a filesystem-safe identifier for hostURL,
// suitable for use as a directory name. It mirrors the input shape of
// the --host flag so client and server compute the same key.
func safeHostName(hostURL *url.URL) string {
	return safeNameRegexp.ReplaceAllString(
		hostURL.Scheme+"://"+hostURL.Host+hostURL.Path, "_",
	)
}

// serverReadyTimeout returns the total budget for the readiness probe.
// Overridable via CRUSH_SERVER_READY_TIMEOUT (parsed as a Go duration).
func serverReadyTimeout() time.Duration {
	const def = 10 * time.Second
	v := os.Getenv("CRUSH_SERVER_READY_TIMEOUT")
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return def
	}
	return d
}

// waitForServerReady polls GET /v1/health until the server responds with
// any 2xx status or the total timeout elapses. Each attempt uses a short
// per-attempt timeout so a hung listener doesn't burn the whole budget.
//
// The HTTP transport is built to mirror how *client.Client dials so the
// same unix socket / npipe / tcp setups all work uniformly here.
func waitForServerReady(ctx context.Context, hostURL *url.URL) error {
	httpClient, reqURL, err := readinessHTTPClient(hostURL)
	if err != nil {
		return err
	}

	const perAttempt = 100 * time.Millisecond
	deadline := time.Now().Add(serverReadyTimeout())

	var lastErr error
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if time.Now().After(deadline) {
			if lastErr != nil {
				return lastErr
			}
			return fmt.Errorf("timed out waiting for server readiness")
		}

		attemptCtx, cancel := context.WithTimeout(ctx, perAttempt)
		err := probeHealth(attemptCtx, httpClient, reqURL, hostURL)
		cancel()
		if err == nil {
			return nil
		}
		lastErr = err

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(perAttempt):
		}
	}
}

// readinessHTTPClient builds an *http.Client whose transport dials the
// server using the same scheme-aware logic as *client.Client (unix
// socket, named pipe, or tcp).
func readinessHTTPClient(hostURL *url.URL) (*http.Client, string, error) {
	c, err := client.NewClient("", hostURL.Scheme, hostURL.Host)
	if err != nil {
		return nil, "", err
	}

	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		return c.Dial(ctx, network, addr)
	}
	if hostURL.Scheme == "unix" || hostURL.Scheme == "npipe" {
		tr.DisableCompression = true
	}

	httpClient := &http.Client{Transport: tr}

	// For unix sockets / named pipes we still need a syntactically valid
	// HTTP URL; the actual address is resolved by the dialer.
	host := hostURL.Host
	if hostURL.Scheme == "unix" || hostURL.Scheme == "npipe" {
		host = client.DummyHost
	}
	reqURL := (&url.URL{Scheme: "http", Host: host, Path: "/v1/health"}).String()
	return httpClient, reqURL, nil
}

// probeHealth issues a single GET to the readiness endpoint and treats
// any 2xx response as success.
func probeHealth(ctx context.Context, h *http.Client, reqURL string, hostURL *url.URL) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return err
	}
	if hostURL.Scheme == "unix" || hostURL.Scheme == "npipe" {
		req.Host = client.DummyHost
	}
	rsp, err := h.Do(req)
	if err != nil {
		return err
	}
	defer rsp.Body.Close()
	_, _ = io.Copy(io.Discard, rsp.Body)
	if rsp.StatusCode < 200 || rsp.StatusCode >= 300 {
		return fmt.Errorf("server health check failed: %s", rsp.Status)
	}
	return nil
}

// restartIfStale checks whether the running server matches the current
// client version. When they differ it asks the server to stand down and,
// if it agrees, removes the stale socket so the caller can start a fresh
// server.
//
// The request is conditional and the server has the last word: it refuses
// while it is hosting anything, because BuildID derives from the
// executable's mtime, so any rebuild (including every `go run`) makes a
// second session look like an upgrade and would otherwise kill the first
// session's workspaces. Servers too old to understand the conditional
// command are left running for the same reason — the request they do
// understand is unconditional. They shut themselves down when they go
// idle, and the next client then finds no socket and spawns a current one.
//
// It returns restarted=true only when the server accepted the shutdown and
// the caller must spawn a replacement.
func restartIfStale(ctx context.Context, hostURL *url.URL) (restarted bool, err error) {
	c, err := client.NewClient("", hostURL.Scheme, hostURL.Host)
	if err != nil {
		return false, err
	}
	vi, err := c.VersionInfo(ctx)
	if err != nil {
		return false, err
	}
	if vi.Version == version.Version && vi.BuildID == version.BuildID {
		return false, nil
	}
	versionFields := []any{
		"server_version", vi.Version,
		"client_version", version.Version,
		"server_build_id", vi.BuildID,
		"client_build_id", version.BuildID,
	}
	// Every refusal — in use, too old to be asked, or unreachable — leads to
	// the same safe outcome: keep using the running server. The wrapped
	// error says which it was.
	if err := c.ShutdownServerIfIdle(ctx); err != nil {
		if !errors.Is(err, client.ErrUnsupported) {
			slog.Warn("Server version differs but it will not stand down; reusing it",
				append(versionFields, "error", err)...)
			return false, nil
		}
		// The server predates shutdown_if_idle. Fall back to the
		// unconditional command, but only after verifying it is idle.
		if !shutdownLegacyStaleServer(ctx, c, versionFields) {
			return false, nil
		}
	}
	slog.Info("Stale server accepted shutdown, restarting", versionFields...)
	if err := awaitSocketGone(ctx, hostURL); err != nil {
		return true, err
	}
	return true, nil
}

// shutdownLegacyStaleServer handles a stale server too old to understand the
// idle-checked shutdown. That server's only "shutdown" command is
// unconditional and would take live sessions down, so it is used only after
// listing workspaces confirms the server is idle. It reports whether the
// server accepted the shutdown; every other outcome (unreachable, busy, or a
// refused shutdown) is logged and reported as false so the caller reuses the
// running server.
func shutdownLegacyStaleServer(ctx context.Context, c *client.Client, versionFields []any) bool {
	workspaces, err := c.ListWorkspaces(ctx)
	if err != nil {
		slog.Warn("Server version differs but it will not stand down; reusing it",
			append(versionFields, "list_error", err)...)
		return false
	}
	if len(workspaces) > 0 {
		slog.Warn("Server version differs and has active workspaces; reusing it",
			append(versionFields, "workspaces", len(workspaces))...)
		return false
	}
	if err := c.ShutdownServer(ctx); err != nil {
		slog.Warn("Server version differs but it will not stand down; reusing it",
			append(versionFields, "error", err)...)
		return false
	}
	return true
}

// awaitSocketGone gives a server that has committed to exiting a moment to
// release its socket, then force-removes whatever is left: the old process
// has latched its decision and will not serve requests again.
func awaitSocketGone(ctx context.Context, hostURL *url.URL) error {
	for range 20 {
		if _, err := os.Stat(hostURL.Host); errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	_ = os.Remove(hostURL.Host)
	return nil
}

var safeNameRegexp = regexp.MustCompile(`[^a-zA-Z0-9._-]`)

func startDetachedServer(hostURL *url.URL, hostArg string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %v", err)
	}

	chDir, err := perHostServerDir(hostURL)
	if err != nil {
		return err
	}

	cmdArgs := []string{"server"}
	if hostArg != server.DefaultHost() {
		cmdArgs = append(cmdArgs, "--host", hostArg)
	}

	// Use context.Background() so the parent's context cancellation does not
	// kill the spawned server. detachProcess (Setsid on !windows,
	// DETACHED_PROCESS on windows) is what truly detaches the child from
	// this process's lifetime.
	c := exec.CommandContext(context.Background(), exe, cmdArgs...)
	stdoutPath := filepath.Join(chDir, "stdout.log")
	stderrPath := filepath.Join(chDir, "stderr.log")
	detachProcess(c)

	stdout, err := os.Create(stdoutPath)
	if err != nil {
		return fmt.Errorf("failed to create stdout log file: %v", err)
	}
	defer stdout.Close()
	c.Stdout = stdout

	stderr, err := os.Create(stderrPath)
	if err != nil {
		return fmt.Errorf("failed to create stderr log file: %v", err)
	}
	defer stderr.Close()
	c.Stderr = stderr

	if err := c.Start(); err != nil {
		return fmt.Errorf("failed to start crush server: %v", err)
	}

	if err := c.Process.Release(); err != nil {
		return fmt.Errorf("failed to detach crush server process: %v", err)
	}

	return nil
}

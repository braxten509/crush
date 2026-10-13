package serverctl

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"net/url"
	"os"
	"time"

	"github.com/charmbracelet/crush/internal/client"
	"github.com/charmbracelet/crush/internal/version"
)

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

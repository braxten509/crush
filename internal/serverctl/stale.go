package serverctl

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/url"
	"os"
	"time"

	"github.com/charmbracelet/crush/internal/backend"
	"github.com/charmbracelet/crush/internal/client"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/version"
)

// checkEngine applies the version rules of engine-design.md 5.1 c to the
// server or engine c talks to:
//
//  1. Another protocol (an engine from before the field reports 0): it is
//     asked to stand down if idle. If it agrees, reuse is false and the
//     caller waits for its socket to go and starts a new one. If it is
//     busy, the error is [backend.ErrProtocolMismatch]: it is never reused.
//  2. The same protocol but another version or build: it is asked to stand
//     down if idle. If it agrees, reuse is false; if it is busy, it is
//     reused, as before.
//  3. Everything equal: reuse.
//
// The request is conditional and the server has the last word: it refuses
// while it is hosting anything, because BuildID derives from the
// executable's mtime, so any rebuild (including every `go run`) makes a
// second session look like an upgrade and would otherwise kill the first
// session's workspaces. Servers too old to understand the conditional
// command are asked with the unconditional one only after listing their
// workspaces shows they are idle.
func checkEngine(ctx context.Context, c *client.Client) (reuse bool, err error) {
	vi, err := c.VersionInfo(ctx)
	if err != nil {
		return false, err
	}
	versionFields := []any{
		"server_protocol", vi.Protocol,
		"client_protocol", proto.ProtocolVersion,
		"server_version", vi.Version,
		"client_version", version.Version,
		"server_build_id", vi.BuildID,
		"client_build_id", version.BuildID,
	}
	if vi.Protocol != proto.ProtocolVersion {
		if standDownIfIdle(ctx, c, versionFields) {
			slog.Info("Server speaks another protocol and accepted shutdown, restarting", versionFields...)
			return false, nil
		}
		return false, &ensureError{
			kind: backend.ErrProtocolMismatch,
			msg: fmt.Sprintf("The Crush engine for this project is busy and speaks protocol %d; this crush speaks %d. Wait for its work to finish or close the apps using it, then try again.",
				vi.Protocol, proto.ProtocolVersion),
		}
	}
	if vi.Version == version.Version && vi.BuildID == version.BuildID {
		return true, nil
	}
	if standDownIfIdle(ctx, c, versionFields) {
		slog.Info("Stale server accepted shutdown, restarting", versionFields...)
		return false, nil
	}
	slog.Info("Server version differs but it is in use; reusing it", versionFields...)
	return true, nil
}

// standDownIfIdle asks the server to shut down if it is idle and reports
// whether it agreed. Every refusal — in use, too old to be asked while
// busy, or unreachable — is logged and reported as false.
func standDownIfIdle(ctx context.Context, c *client.Client, versionFields []any) bool {
	err := c.ShutdownServerIfIdle(ctx)
	if err == nil {
		return true
	}
	if errors.Is(err, client.ErrUnsupported) {
		// The server predates shutdown_if_idle. Fall back to the
		// unconditional command, but only after verifying it is idle.
		return shutdownLegacyStaleServer(ctx, c, versionFields)
	}
	slog.Warn("Server version differs but it will not stand down",
		append(versionFields, "error", err)...)
	return false
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

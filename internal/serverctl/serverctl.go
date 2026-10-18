// Package serverctl starts, health-checks and replaces the detached Crush
// server that client/server mode talks to.
package serverctl

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/charmbracelet/crush/internal/backend"
	"github.com/charmbracelet/crush/internal/client"
	"github.com/charmbracelet/crush/internal/config"
	crushlog "github.com/charmbracelet/crush/internal/log"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/server"
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
			c, err := client.NewClient("", hostURL.Scheme, hostURL.Host)
			if err != nil {
				return err
			}
			reuse, err := checkEngine(ctx, c)
			switch {
			case errors.Is(err, backend.ErrProtocolMismatch):
				// A busy server of another protocol is never reused.
				return err
			case err != nil:
				slog.Warn("Failed to check server version", "error", err)
				needsStart = true
			case !reuse:
				if err := awaitSocketGone(ctx, hostURL); err != nil {
					return err
				}
				needsStart = true
			}
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

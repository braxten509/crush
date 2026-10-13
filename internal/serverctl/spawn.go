package serverctl

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"time"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/lock"
	"github.com/charmbracelet/crush/internal/server"
)

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

var safeNameRegexp = regexp.MustCompile(`[^a-zA-Z0-9._-]`)

// safeHostName returns a filesystem-safe identifier for hostURL,
// suitable for use as a directory name. It mirrors the input shape of
// the --host flag so client and server compute the same key.
func safeHostName(hostURL *url.URL) string {
	return safeNameRegexp.ReplaceAllString(
		hostURL.Scheme+"://"+hostURL.Host+hostURL.Path, "_",
	)
}

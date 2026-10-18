package serverctl

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/crush/internal/backend"
	"github.com/charmbracelet/crush/internal/client"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/db"
	"github.com/charmbracelet/crush/internal/lock"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/server"
)

// EnsureOptions says which project's engine to find or start.
type EnsureOptions struct {
	Cwd     string        // the app's folder
	DataDir string        // explicit --data-dir, or ""
	Debug   bool          // start the engine with --debug
	Timeout time.Duration // 0 means 15 s (2 s for FindEngine)
}

// EngineHandle is a running engine of the project.
type EngineHandle struct {
	Address, Socket string
	Protocol        int
	Version         string
	PID             int
	DataDir         string
	// Started is true when this call started the engine.
	Started bool
}

// Ensure failures, matched with errors.Is. Their text is the message for
// people, as the desktop app shows it.
var (
	// ErrProjectLocked wraps the project lock's message (engine-design.md
	// 5.10).
	ErrProjectLocked = errors.New("project locked")
	// ErrEngineStart wraps why the engine did not start.
	ErrEngineStart = errors.New("engine did not start")
)

const (
	defaultEnsureTimeout = 15 * time.Second
	defaultFindTimeout   = 2 * time.Second
	// versionProbeTimeout bounds the first version request to an engine.
	versionProbeTimeout = 2 * time.Second
	// projectLockPoll is how often ensure checks whether an old engine let
	// go of the project.
	projectLockPoll = 100 * time.Millisecond
	// engineReadyPoll is how often ensure checks a starting engine.
	engineReadyPoll = 100 * time.Millisecond
	// engineLogTailLines is how much of engine.log a start failure shows.
	engineLogTailLines = 20

	startErrorFileName = "start-error.json"
	engineLogFileName  = "engine.log"
)

// ensureError is an ensure failure whose text is exactly its message, and
// which matches kind (and cause, when set) with errors.Is.
type ensureError struct {
	kind  error
	msg   string
	cause error
}

func (e *ensureError) Error() string { return e.msg }

func (e *ensureError) Unwrap() []error {
	if e.cause != nil {
		return []error{e.kind, e.cause}
	}
	return []error{e.kind}
}

// StartError is what an engine that could not start leaves for the ensure
// that started it, in start-error.json.
type StartError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// engineHostURL is the --host form of an engine socket.
func engineHostURL(socket string) *url.URL {
	return &url.URL{Scheme: "unix", Host: socket}
}

// EngineStateDir is the folder that holds an engine's start lock, its logs
// and start-error.json: the per-host folder of its socket.
func EngineStateDir(socket string) (string, error) {
	return perHostServerDir(engineHostURL(socket))
}

// EngineLogFile is the log file the engine at socket writes to.
func EngineLogFile(socket string) string {
	return LogFile(engineHostURL(socket))
}

// WriteStartError records why the engine at socket could not start.
func WriteStartError(socket string, e StartError) error {
	dir, err := EngineStateDir(socket)
	if err != nil {
		return err
	}
	data, err := json.Marshal(e)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, startErrorFileName), data, 0o600)
}

// readStartError reads start-error.json from dir, if there is one.
func readStartError(dir string) (StartError, bool) {
	data, err := os.ReadFile(filepath.Join(dir, startErrorFileName))
	if err != nil {
		return StartError{}, false
	}
	var e StartError
	if json.Unmarshal(data, &e) != nil || e.Message == "" {
		return StartError{}, false
	}
	return e, true
}

// resolveEngineDataDir returns the canonical data folder of the project
// the options name, and the folder the app works in.
func resolveEngineDataDir(opts EnsureOptions) (dataDir, cwd string, err error) {
	cwd = opts.Cwd
	if cwd == "" {
		if cwd, err = os.Getwd(); err != nil {
			return "", "", fmt.Errorf("failed to get the current folder: %w", err)
		}
	}
	resolved, err := config.ResolveDataDir(cwd, opts.DataDir)
	if err != nil {
		return "", "", err
	}
	dataDir, err = config.CanonicalDataDir(resolved)
	if err != nil {
		return "", "", fmt.Errorf("failed to resolve the data folder %q: %w", resolved, err)
	}
	return dataDir, cwd, nil
}

// engineState is what a probe found at the project's socket.
type engineState int

const (
	// engineGone: nothing answers (no socket file, refused, stale), or the
	// engine is on its way out (closing, 503, or it agreed to stand down).
	engineGone engineState = iota
	// engineUsable: a live engine of this project that this crush can use.
	engineUsable
)

// probeEngine runs ensure's steps 3 and 4: the version rules of
// engine-design.md 5.1 c, then the engine's project and closing state.
func probeEngine(ctx context.Context, c *client.Client, socket, dataDir string) (EngineHandle, engineState, error) {
	versionCtx, cancel := context.WithTimeout(ctx, versionProbeTimeout)
	reuse, err := checkEngine(versionCtx, c)
	cancel()
	switch {
	case err != nil && client.IsUnreachable(err):
		return EngineHandle{}, engineGone, nil
	case err != nil:
		if errors.Is(err, backend.ErrProtocolMismatch) {
			return EngineHandle{}, engineGone, err
		}
		return EngineHandle{}, engineGone, fmt.Errorf("the Crush engine at %s does not answer: %w", socket, err)
	case !reuse:
		// It agreed to stand down: give it a moment to let go of the
		// socket. The project lock wait covers the rest.
		if err := awaitSocketGone(ctx, engineHostURL(socket)); err != nil {
			return EngineHandle{}, engineGone, err
		}
		return EngineHandle{}, engineGone, nil
	}
	info, err := c.EngineInfo(ctx)
	if err != nil {
		if client.IsUnreachable(err) {
			return EngineHandle{}, engineGone, nil
		}
		return EngineHandle{}, engineGone, fmt.Errorf("the Crush engine at %s does not answer: %w", socket, err)
	}
	if info.DataDir != dataDir {
		return EngineHandle{}, engineGone, fmt.Errorf("%w: the Crush engine at %s serves the project whose data folder is %s, not %s",
			backend.ErrWrongProject, socket, info.DataDir, dataDir)
	}
	if info.Closing {
		return EngineHandle{}, engineGone, nil
	}
	return engineHandle(info, socket, false), engineUsable, nil
}

func engineHandle(info proto.EngineInfo, socket string, started bool) EngineHandle {
	return EngineHandle{
		Address:  "unix://" + socket,
		Socket:   socket,
		Protocol: info.Protocol,
		Version:  info.Version,
		PID:      info.PID,
		DataDir:  info.DataDir,
		Started:  started,
	}
}

// EnsureEngine finds the running engine of the project the options name,
// or starts one (engine-design.md 5.2):
//
//  1. It resolves and canonicalizes the data folder.
//  2. It finds the socket path, checking its folder.
//  3. It asks the engine its version and applies the protocol and build
//     rules of 5.1 c: a busy engine of another protocol is an
//     ErrProtocolMismatch, and an idle out-of-date one is replaced.
//  4. It requires the engine to serve this project, and returns it unless
//     it is closing.
//  5. Otherwise, holding start.lock, it checks again, waits for an old
//     engine to let go of the project lock, and starts
//     `<exe> engine serve --data-dir <folder>` detached, with its output in
//     engine.log, then waits until it is ready.
//  6. A start that fails reports start-error.json (ErrProjectLocked or
//     ErrEngineStart), or the end of engine.log.
func EnsureEngine(ctx context.Context, opts EnsureOptions) (EngineHandle, error) {
	ctx, cancel := context.WithTimeout(ctx, cmp.Or(opts.Timeout, defaultEnsureTimeout))
	defer cancel()

	dataDir, cwd, err := resolveEngineDataDir(opts)
	if err != nil {
		return EngineHandle{}, err
	}
	socket, err := server.EngineSocketPath(dataDir)
	if err != nil {
		return EngineHandle{}, err
	}
	c, err := client.NewClient(cwd, "unix", socket)
	if err != nil {
		return EngineHandle{}, err
	}
	if h, state, err := probeEngine(ctx, c, socket, dataDir); err != nil || state == engineUsable {
		return h, err
	}

	stateDir, err := EngineStateDir(socket)
	if err != nil {
		return EngineHandle{}, err
	}
	release, err := lock.File(ctx, filepath.Join(stateDir, "start.lock"))
	if err != nil {
		return EngineHandle{}, &ensureError{kind: ErrEngineStart, msg: "the Crush engine did not start: another start of it did not finish", cause: err}
	}
	defer release()

	// Another ensure may have started it while this one waited.
	if h, state, err := probeEngine(ctx, c, socket, dataDir); err != nil || state == engineUsable {
		return h, err
	}
	if err := waitForProjectLock(ctx, dataDir); err != nil {
		return EngineHandle{}, err
	}
	if err := os.Remove(filepath.Join(stateDir, startErrorFileName)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return EngineHandle{}, err
	}
	exited, err := startEngine(stateDir, dataDir, opts.Debug)
	if err != nil {
		return EngineHandle{}, &ensureError{kind: ErrEngineStart, msg: "the Crush engine did not start: " + err.Error(), cause: err}
	}
	if err := waitForEngineReady(ctx, socket, stateDir, exited); err != nil {
		return EngineHandle{}, err
	}
	info, err := c.EngineInfo(ctx)
	if err != nil {
		return EngineHandle{}, &ensureError{kind: ErrEngineStart, msg: "the Crush engine started but does not answer: " + err.Error(), cause: err}
	}
	if info.DataDir != dataDir {
		return EngineHandle{}, fmt.Errorf("%w: the Crush engine at %s serves the project whose data folder is %s, not %s",
			backend.ErrWrongProject, socket, info.DataDir, dataDir)
	}
	return engineHandle(info, socket, true), nil
}

// FindEngine runs EnsureEngine's steps 1 to 4 and never starts an engine,
// nor asks one to stand down. It reports ok=false when no engine answers
// within opts.Timeout (0 means 2 s), or when the one that answers is
// closing or speaks another protocol.
func FindEngine(ctx context.Context, opts EnsureOptions) (h EngineHandle, ok bool, err error) {
	ctx, cancel := context.WithTimeout(ctx, cmp.Or(opts.Timeout, defaultFindTimeout))
	defer cancel()

	dataDir, cwd, err := resolveEngineDataDir(opts)
	if err != nil {
		return EngineHandle{}, false, err
	}
	socket, err := server.EngineSocketPath(dataDir)
	if err != nil {
		return EngineHandle{}, false, err
	}
	c, err := client.NewClient(cwd, "unix", socket)
	if err != nil {
		return EngineHandle{}, false, err
	}
	vi, err := c.VersionInfo(ctx)
	if err != nil || vi.Protocol != proto.ProtocolVersion {
		return EngineHandle{}, false, nil
	}
	info, err := c.EngineInfo(ctx)
	if err != nil {
		return EngineHandle{}, false, nil
	}
	if info.DataDir != dataDir {
		return EngineHandle{}, false, fmt.Errorf("%w: the Crush engine at %s serves the project whose data folder is %s, not %s",
			backend.ErrWrongProject, socket, info.DataDir, dataDir)
	}
	if info.Closing {
		return EngineHandle{}, false, nil
	}
	return engineHandle(info, socket, false), true, nil
}

// waitForProjectLock waits while another process holds the project lock
// exclusively: an old engine on its way out holds it until its process
// ends, after its socket is gone. A lone window's shared lock does not
// block the wait; the new engine reports it. When ctx ends first, it
// returns ErrProjectLocked with the lock's message.
func waitForProjectLock(ctx context.Context, dataDir string) error {
	for {
		err := db.ProbeProjectLock(dataDir)
		if err == nil {
			return nil
		}
		if !errors.Is(err, db.ErrDataDirLocked) {
			return err
		}
		select {
		case <-ctx.Done():
			return &ensureError{kind: ErrProjectLocked, msg: err.Error(), cause: err}
		case <-time.After(projectLockPoll):
		}
	}
}

// startEngine starts `<exe> engine serve` for dataDir, detached, with its
// output in engine.log. The returned channel closes when the process ends.
func startEngine(stateDir, dataDir string, debug bool) (<-chan struct{}, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("failed to get executable path: %v", err)
	}
	args := []string{"engine", "serve", "--data-dir", dataDir}
	if debug {
		args = append(args, "--debug")
	}
	// Not bound to the caller's context: the engine outlives this process.
	// detachProcess starts it in its own session.
	cmd := exec.CommandContext(context.Background(), exe, args...)
	detachProcess(cmd)
	logFile, err := os.Create(filepath.Join(stateDir, engineLogFileName))
	if err != nil {
		return nil, fmt.Errorf("failed to create the engine log: %v", err)
	}
	defer logFile.Close()
	cmd.Stdout, cmd.Stderr = logFile, logFile
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	slog.Info("Started the Crush engine", "pid", cmd.Process.Pid, "data_dir", dataDir)
	exited := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(exited)
	}()
	return exited, nil
}

// waitForEngineReady waits until the engine at socket answers its health
// check. When the engine ends first, or ctx ends, it reports the engine's
// start-error.json, or the end of engine.log.
func waitForEngineReady(ctx context.Context, socket, stateDir string, exited <-chan struct{}) error {
	hostURL := engineHostURL(socket)
	for {
		probeCtx, cancel := context.WithTimeout(ctx, engineReadyPoll)
		err := quickHealthProbe(probeCtx, hostURL)
		cancel()
		if err == nil {
			return nil
		}
		select {
		case <-exited:
			return engineStartFailure(stateDir)
		case <-ctx.Done():
			return engineStartFailure(stateDir)
		case <-time.After(engineReadyPoll):
		}
	}
}

// engineStartFailure explains why an engine did not become ready.
func engineStartFailure(stateDir string) error {
	if e, ok := readStartError(stateDir); ok {
		kind := ErrEngineStart
		if e.Code == "project_locked" {
			kind = ErrProjectLocked
		}
		return &ensureError{kind: kind, msg: e.Message}
	}
	msg := "the Crush engine did not start"
	if tail := logTail(filepath.Join(stateDir, engineLogFileName), engineLogTailLines); tail != "" {
		msg += ". The end of its log:\n" + tail
	}
	return &ensureError{kind: ErrEngineStart, msg: msg}
}

// logTail returns the last n lines of the file at path.
func logTail(path string, n int) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimRight(string(bytes.TrimSpace(data)), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

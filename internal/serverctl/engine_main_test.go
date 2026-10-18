//go:build !windows

package serverctl_test

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/charmbracelet/crush/internal/client"
	"github.com/charmbracelet/crush/internal/cmd"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/proto/prototest"
	"github.com/charmbracelet/crush/internal/server"
	"github.com/charmbracelet/crush/internal/serverctl"
	"github.com/stretchr/testify/require"
)

// TestMain lets this test binary stand in for crush. EnsureEngine starts
// `<exe> engine serve --data-dir D`, and <exe> is this binary, so a run
// whose first argument is "engine" is the crush command line, not a test
// run. The same holds for `engine ensure`, which the tests also run.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "engine" {
		cmd.Execute()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// fakeEnginePID is the PID a fake engine reports. Nothing ever signals it.
const fakeEnginePID = 999999

// engineProject is a project in a throwaway home: every folder an engine
// and its ensure use (config, data, cache, runtime, temp) is under root,
// which is short enough for the engine socket's path.
type engineProject struct {
	root         string
	cwd          string // the project folder, without git
	dataDir      string // its canonical data folder
	socket       string // its engine socket
	globalConfig string // the folder of the global crush.json

	mu      sync.Mutex
	started []int // PIDs of engines this test's ensures started
}

// newEngineProject sets the environment for one engine test: idle is
// CRUSH_ENGINE_IDLE_TIMEOUT ("" keeps the 10 s default). Every engine the
// test started is killed at cleanup.
func newEngineProject(t *testing.T, idle string) *engineProject {
	t.Helper()
	root, err := os.MkdirTemp("", "e")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(root) })

	home := filepath.Join(root, "h")
	runtimeDir := filepath.Join(root, "r")
	tmp := filepath.Join(root, "t")
	globalConfig := filepath.Join(root, "g")
	for _, dir := range []string{home, tmp, globalConfig, filepath.Join(root, "gd")} {
		require.NoError(t, os.MkdirAll(dir, 0o700))
	}
	require.NoError(t, os.Mkdir(runtimeDir, 0o700))
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("CRUSH_CACHE_DIR", filepath.Join(home, ".cache", "crush"))
	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)
	t.Setenv("TMPDIR", tmp)
	t.Setenv("CRUSH_GLOBAL_CONFIG", globalConfig)
	t.Setenv("CRUSH_GLOBAL_DATA", filepath.Join(root, "gd"))
	t.Setenv("CRUSH_DISABLE_PROVIDER_AUTO_UPDATE", "1")
	t.Setenv("CRUSH_DISABLE_METRICS", "1")
	t.Setenv("DO_NOT_TRACK", "1")
	t.Setenv("CRUSH_SKIP_DATADIR_LOCK", "")
	t.Setenv("CRUSH_CLIENT_SERVER", "")
	t.Setenv("CRUSH_ENGINE_IDLE_TIMEOUT", idle)
	// The engine joins the memory box first; no test may move a process
	// into the user's real one.
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path=/nonexistent")
	writeGlobalConfig(t, globalConfig, nil)

	p := &engineProject{root: root, cwd: filepath.Join(root, "p"), globalConfig: globalConfig}
	require.NoError(t, os.Mkdir(p.cwd, 0o700))
	resolved, err := config.ResolveDataDir(p.cwd, "")
	require.NoError(t, err)
	p.dataDir, err = config.CanonicalDataDir(resolved)
	require.NoError(t, err)
	p.socket, err = server.EngineSocketPath(p.dataDir)
	require.NoError(t, err)
	t.Cleanup(func() { p.killEngines(t) })
	return p
}

// writeGlobalConfig writes the global crush.json: the embedded provider
// catalog without a network refresh, plus extra's top-level keys.
func writeGlobalConfig(t *testing.T, dir string, extra map[string]any) {
	t.Helper()
	cfg := map[string]any{
		"options": map[string]any{"disable_provider_auto_update": true},
	}
	for k, v := range extra {
		cfg[k] = v
	}
	data, err := json.Marshal(cfg)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "crush.json"), data, 0o600))
}

func (p *engineProject) opts() serverctl.EnsureOptions {
	return serverctl.EnsureOptions{Cwd: p.cwd}
}

// ensure runs EnsureEngine for the project and remembers an engine it
// started, so cleanup can stop it.
func (p *engineProject) ensure(ctx context.Context) (serverctl.EngineHandle, error) {
	return p.ensureWith(ctx, p.opts())
}

func (p *engineProject) ensureWith(ctx context.Context, opts serverctl.EnsureOptions) (serverctl.EngineHandle, error) {
	h, err := serverctl.EnsureEngine(ctx, opts)
	if err == nil && h.Started {
		p.mu.Lock()
		p.started = append(p.started, h.PID)
		p.mu.Unlock()
	}
	return h, err
}

// create joins the engine h as an app does. A create that meets the
// engine's exit runs ensure again, as the terminal's switch does.
func (p *engineProject) create(t *testing.T, h serverctl.EngineHandle) (*client.Client, *proto.Workspace) {
	t.Helper()
	c, err := client.NewClient(p.cwd, "unix", h.Socket)
	require.NoError(t, err)
	ws, err := serverctl.CreateWorkspace(t.Context(), c, prototest.CreateRequest(proto.Workspace{Path: p.cwd}), func() error {
		_, err := p.ensure(t.Context())
		return err
	})
	require.NoError(t, err)
	return c, ws
}

// engineInfo asks the project's engine what it is.
func (p *engineProject) engineInfo(t *testing.T) (proto.EngineInfo, error) {
	t.Helper()
	c, err := client.NewClient(p.cwd, "unix", p.socket)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	return c.EngineInfo(ctx)
}

// killEngines stops every engine the test started, and a real engine
// still answering on the project's socket.
func (p *engineProject) killEngines(t *testing.T) {
	p.mu.Lock()
	pids := append([]int(nil), p.started...)
	p.mu.Unlock()
	c, err := client.NewClient(p.cwd, "unix", p.socket)
	if err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		if info, err := c.EngineInfo(ctx); err == nil && info.PID != fakeEnginePID && info.PID != os.Getpid() {
			pids = append(pids, info.PID)
		}
		cancel()
	}
	for _, pid := range pids {
		killEngine(t, pid)
	}
}

// killEngine kills the engine with pid, as a crash does, and waits until
// it is gone.
func killEngine(t *testing.T, pid int) {
	t.Helper()
	if pid <= 0 || pid == os.Getpid() {
		return
	}
	_ = syscall.Kill(pid, syscall.SIGKILL)
	require.Eventually(t, func() bool { return processGone(pid) }, 10*time.Second, 20*time.Millisecond,
		"engine %d did not exit", pid)
}

// processGone reports whether no process with pid exists (an engine this
// test started is reaped by the goroutine that started it).
func processGone(pid int) bool {
	return errors.Is(syscall.Kill(pid, 0), syscall.ESRCH)
}

// fakeEngineOpts describes a fake engine.
type fakeEngineOpts struct {
	vi proto.VersionInfo
	// busy makes it refuse shutdown_if_idle, as an engine hosting a
	// workspace does.
	busy bool
	// onCreate answers POST /v1/workspaces; nil answers 500.
	onCreate func(f *fakeEngine, w http.ResponseWriter)
}

// fakeEngine answers on the project's engine socket as an engine of some
// other build or protocol does.
type fakeEngine struct {
	opts    fakeEngineOpts
	dataDir string
	ln      net.Listener

	mu       sync.Mutex
	commands []string
	creates  int
	closing  bool
	stopOnce sync.Once
}

// startFakeEngine listens on the project's engine socket.
func (p *engineProject) startFakeEngine(t *testing.T, opts fakeEngineOpts) *fakeEngine {
	t.Helper()
	ln, err := net.Listen("unix", p.socket)
	require.NoError(t, err)
	f := &fakeEngine{opts: opts, dataDir: p.dataDir, ln: ln}
	srv := &http.Server{Handler: http.HandlerFunc(f.serve), ReadHeaderTimeout: 5 * time.Second}
	// One request per connection, so a client never reaches the fake again
	// on a kept-alive connection after it stopped listening.
	srv.SetKeepAlivesEnabled(false)
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	return f
}

func (f *fakeEngine) serve(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/v1/version":
		_ = json.NewEncoder(w).Encode(f.opts.vi)
	case r.URL.Path == "/v1/engine":
		f.mu.Lock()
		closing := f.closing
		f.mu.Unlock()
		_ = json.NewEncoder(w).Encode(proto.EngineInfo{
			Protocol: f.opts.vi.Protocol, Version: f.opts.vi.Version, PID: fakeEnginePID,
			DataDir: f.dataDir, Closing: closing,
		})
	case r.URL.Path == "/v1/workspaces" && r.Method == http.MethodPost:
		f.mu.Lock()
		f.creates++
		f.mu.Unlock()
		if f.opts.onCreate == nil {
			http.Error(w, "no creates here", http.StatusInternalServerError)
			return
		}
		f.opts.onCreate(f, w)
	case r.URL.Path == "/v1/workspaces":
		n := 0
		if f.opts.busy {
			n = 1
		}
		_ = json.NewEncoder(w).Encode(make([]proto.Workspace, n))
	case r.URL.Path == "/v1/control":
		var req proto.ServerControl
		_ = json.NewDecoder(r.Body).Decode(&req)
		f.mu.Lock()
		f.commands = append(f.commands, req.Command)
		f.mu.Unlock()
		if f.opts.busy {
			http.Error(w, "server is hosting live workspaces", http.StatusConflict)
			return
		}
		w.WriteHeader(http.StatusOK)
		f.stopListening()
	default:
		http.NotFound(w, r)
	}
}

// stopListening closes the fake's listener, which removes its socket file,
// as an engine on its way out does.
func (f *fakeEngine) stopListening() {
	f.stopOnce.Do(func() { _ = f.ln.Close() })
}

func (f *fakeEngine) setClosing() {
	f.mu.Lock()
	f.closing = true
	f.mu.Unlock()
}

func (f *fakeEngine) commandsSeen() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.commands...)
}

func (f *fakeEngine) createCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.creates
}

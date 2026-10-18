//go:build !windows

package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/charmbracelet/crush/internal/backend"
	"github.com/charmbracelet/crush/internal/client"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/db"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/server"
	"github.com/charmbracelet/crush/internal/serverctl"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

// TestMain lets this test binary stand in for crush. The engines and the
// legacy server the tests start are this binary run as `crush engine
// serve` and `crush server`, and the tests run `crush engine` and `crush
// logout` through it too.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "engine", "server", "logout":
			// No test process joins the user's real memory box.
			joinMemoryBox = func(string) error { return nil }
			Execute()
			os.Exit(0)
		}
	}
	os.Exit(m.Run())
}

// cmdProject is a project in a throwaway home: every folder crush uses is
// under root, which is short enough for socket paths (newCmdProject checks).
type cmdProject struct {
	root         string
	cwd          string // the project folder, without git
	dataDir      string // its canonical data folder
	socket       string // its engine socket
	runtimeDir   string // XDG_RUNTIME_DIR
	globalConfig string // the folder of the global crush.json
	globalData   string // the folder of the global data crush.json, where sign-ins go

	mu   sync.Mutex
	pids []int // engines to kill at cleanup
}

// newCmdProject sets the environment for one test and kills the engines it
// started at cleanup.
func newCmdProject(t *testing.T) *cmdProject {
	t.Helper()
	root, err := os.MkdirTemp(testRootBase(), "c")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(root) })

	p := &cmdProject{
		root:         root,
		cwd:          filepath.Join(root, "p"),
		runtimeDir:   filepath.Join(root, "r"),
		globalConfig: filepath.Join(root, "g"),
		globalData:   filepath.Join(root, "gd"),
	}
	home := filepath.Join(root, "h")
	for _, dir := range []string{home, p.cwd, p.globalConfig, p.globalData, filepath.Join(root, "t")} {
		require.NoError(t, os.MkdirAll(dir, 0o700))
	}
	require.NoError(t, os.Mkdir(p.runtimeDir, 0o700))
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("CRUSH_CACHE_DIR", filepath.Join(home, ".cache", "crush"))
	t.Setenv("XDG_RUNTIME_DIR", p.runtimeDir)
	t.Setenv("TMPDIR", filepath.Join(root, "t"))
	t.Setenv("CRUSH_GLOBAL_CONFIG", p.globalConfig)
	t.Setenv("CRUSH_GLOBAL_DATA", p.globalData)
	t.Setenv("CRUSH_DISABLE_PROVIDER_AUTO_UPDATE", "1")
	t.Setenv("CRUSH_DISABLE_METRICS", "1")
	t.Setenv("DO_NOT_TRACK", "1")
	t.Setenv("CRUSH_SKIP_DATADIR_LOCK", "")
	t.Setenv("CRUSH_CLIENT_SERVER", "")
	t.Setenv("CRUSH_ENGINE_IDLE_TIMEOUT", "")
	t.Setenv("CRUSH_ENGINE_FIRST_JOIN_GRACE", "")
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path=/nonexistent")
	writeTestGlobalConfig(t, p.globalConfig, nil)

	socketDir := requireShortSocketFolder(t, p.runtimeDir, filepath.Join(root, "t"))
	p.dataDir, p.socket = projectEngine(t, p.cwd)
	require.Equal(t, socketDir, filepath.Dir(p.socket), "the engine socket is not in the test's socket folder")
	t.Cleanup(func() { p.killEngines(t) })
	return p
}

// testRootBase is the folder the tests' roots go in: the system temp
// folder, but /tmp on macOS, whose own temp folder is long enough to push
// an engine socket under it over the 104-byte limit.
func testRootBase() string {
	if runtime.GOOS == "darwin" {
		return "/tmp"
	}
	return ""
}

// requireShortSocketFolder returns the test's socket folder, the one
// PrivateSocketDir picks under runtimeDir (XDG_RUNTIME_DIR, Linux) or tmp
// (TMPDIR). It fails, before anything looks for the folder, when an engine
// socket in it would be over the 104-byte limit: PrivateSocketDir would
// then fall back to the real /tmp/crush-<uid>.
func requireShortSocketFolder(t *testing.T, runtimeDir, tmp string) string {
	t.Helper()
	dir := filepath.Join(tmp, fmt.Sprintf("crush-%d", os.Getuid()))
	if runtime.GOOS == "linux" {
		dir = filepath.Join(runtimeDir, "crush")
	}
	socket := filepath.Join(dir, "engine-0123456789abcdef.sock")
	require.LessOrEqual(t, len(socket), 104,
		"the test root is too long: the engine socket %s would be over 104 bytes, and the real /tmp/crush-%d would be used instead",
		socket, os.Getuid())
	return dir
}

// unsafeSocketEnv is the environment of a crush that finds no safe socket
// folder: every candidate is a symlink.
func unsafeSocketEnv(t *testing.T, root string) []string {
	t.Helper()
	unsafe := filepath.Join(root, "u")
	target := filepath.Join(unsafe, "target")
	runtimeDir := filepath.Join(unsafe, "r")
	tmp := filepath.Join(unsafe, "t")
	for _, dir := range []string{target, runtimeDir, tmp} {
		require.NoError(t, os.MkdirAll(dir, 0o700))
	}
	require.NoError(t, os.Symlink(target, filepath.Join(runtimeDir, "crush")))
	require.NoError(t, os.Symlink(target, filepath.Join(tmp, fmt.Sprintf("crush-%d", os.Getuid()))))
	return append(os.Environ(), "XDG_RUNTIME_DIR="+runtimeDir, "TMPDIR="+tmp)
}

// defaultSocket is the legacy server's socket in the test's environment,
// the one server.DefaultHost gives.
func defaultSocket(t *testing.T) string {
	t.Helper()
	host := server.DefaultHost()
	require.NotEmpty(t, host, "no default host")
	u, err := server.ParseHostURL(host)
	require.NoError(t, err)
	require.Equal(t, "unix", u.Scheme)
	return u.Host
}

// dirNames lists the names in dir.
func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	names := []string{}
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// projectEngine returns the canonical data folder and engine socket of the
// project in cwd.
func projectEngine(t *testing.T, cwd string) (dataDir, socket string) {
	t.Helper()
	resolved, err := config.ResolveDataDir(cwd, "")
	require.NoError(t, err)
	dataDir, err = config.CanonicalDataDir(resolved)
	require.NoError(t, err)
	socket, err = server.EngineSocketPath(dataDir)
	require.NoError(t, err)
	return dataDir, socket
}

// writeTestGlobalConfig writes the global crush.json: the embedded
// provider catalog without a network refresh, plus extra's top-level keys.
func writeTestGlobalConfig(t *testing.T, dir string, extra map[string]any) {
	t.Helper()
	cfg := map[string]any{"options": map[string]any{"disable_provider_auto_update": true}}
	maps.Copy(cfg, extra)
	data, err := json.Marshal(cfg)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "crush.json"), data, 0o600))
}

// track kills the engine pid at cleanup.
func (p *cmdProject) track(pid int) {
	p.mu.Lock()
	p.pids = append(p.pids, pid)
	p.mu.Unlock()
}

// ensure finds or starts the engine of the project in cwd.
func (p *cmdProject) ensure(t *testing.T, cwd string) serverctl.EngineHandle {
	t.Helper()
	h, err := serverctl.EnsureEngine(t.Context(), serverctl.EnsureOptions{Cwd: cwd})
	require.NoError(t, err)
	p.track(h.PID)
	return h
}

// killEngines kills the tracked engines and one still answering on the
// project's socket.
func (p *cmdProject) killEngines(t *testing.T) {
	p.mu.Lock()
	pids := slices.Clone(p.pids)
	p.mu.Unlock()
	if c, err := client.NewClient(p.cwd, "unix", p.socket); err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		if info, err := c.EngineInfo(ctx); err == nil {
			pids = append(pids, info.PID)
		}
		cancel()
	}
	for _, pid := range pids {
		if pid <= 0 || pid == os.Getpid() {
			continue
		}
		_ = syscall.Kill(pid, syscall.SIGKILL)
		require.Eventually(t, func() bool { return errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) },
			10*time.Second, 20*time.Millisecond, "engine %d did not exit", pid)
	}
}

// runCrush runs this binary as crush with args in dir, and returns its
// stdout, stderr and exit code.
func runCrush(t *testing.T, dir string, env []string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	exe, err := os.Executable()
	require.NoError(t, err)
	var out, errOut bytes.Buffer
	c := exec.CommandContext(t.Context(), exe, args...)
	c.Dir = dir
	c.Env = env
	c.Stdout, c.Stderr = &out, &errOut
	err = c.Run()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exitErr):
		code = exitErr.ExitCode()
	default:
		require.NoError(t, err)
	}
	return out.String(), errOut.String(), code
}

// jsonKeys returns the sorted keys of the JSON object in s.
func jsonKeys(t *testing.T, s string) []string {
	t.Helper()
	var obj map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(s), &obj), "not one JSON object: %s", s)
	return slices.Sorted(maps.Keys(obj))
}

// TestEngineEnsureJSON pins `crush engine ensure`'s output: the exact keys
// and exit code of its success and of each error, then a real run that
// starts the engine, a second run that finds it, and a real run that finds
// no safe socket folder.
func TestEngineEnsureJSON(t *testing.T) {
	handle := serverctl.EngineHandle{
		Address: "unix:///r/crush/engine-0123456789abcdef.sock", Socket: "/r/crush/engine-0123456789abcdef.sock",
		Protocol: proto.ProtocolVersion, Version: "v1.2.3", PID: 4242, DataDir: "/p/.crush", Started: true,
	}
	cases := []struct {
		name string
		err  error
		want string
		exit int
	}{
		{
			name: "success",
			want: fmt.Sprintf(`{"address":"unix:///r/crush/engine-0123456789abcdef.sock","socket":"/r/crush/engine-0123456789abcdef.sock","protocol":%d,"version":"v1.2.3","pid":4242,"data_dir":"/p/.crush","started":true}`, proto.ProtocolVersion),
		},
		{name: "protocol", err: fmt.Errorf("%w: busy", backend.ErrProtocolMismatch), want: `{"error":{"code":"protocol_mismatch","message":"protocol mismatch: busy"}}`, exit: 3},
		{name: "locked", err: fmt.Errorf("%w: held", serverctl.ErrProjectLocked), want: `{"error":{"code":"project_locked","message":"project locked: held"}}`, exit: 4},
		{name: "start", err: fmt.Errorf("%w: log", serverctl.ErrEngineStart), want: `{"error":{"code":"start_failed","message":"engine did not start: log"}}`, exit: 5},
		{name: "socket folder", err: fmt.Errorf("%w: unsafe", server.ErrNoPrivateSocketDir), want: `{"error":{"code":"unsafe_socket_dir","message":"no private folder for the Crush socket: unsafe"}}`, exit: 6},
		{name: "wrong project", err: fmt.Errorf("%w: other", backend.ErrWrongProject), want: `{"error":{"code":"wrong_project","message":"wrong project: other"}}`, exit: 1},
		{name: "other", err: errors.New("boom"), want: `{"error":{"code":"internal","message":"boom"}}`, exit: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := handle
			if tc.err != nil {
				h = serverctl.EngineHandle{}
			}
			out, exit := engineEnsureOutput(h, tc.err)
			var buf bytes.Buffer
			require.NoError(t, writeEngineJSON(&buf, out))
			require.Equal(t, tc.want+"\n", buf.String())
			require.Equal(t, tc.exit, exit)
		})
	}

	p := newCmdProject(t)
	successKeys := []string{"address", "data_dir", "pid", "protocol", "socket", "started", "version"}
	type success struct {
		Socket  string `json:"socket"`
		PID     int    `json:"pid"`
		DataDir string `json:"data_dir"`
		Started bool   `json:"started"`
	}

	stdout, stderr, code := runCrush(t, p.root, os.Environ(), "engine", "ensure", "--cwd", p.cwd)
	require.Equal(t, 0, code, "stderr: %s", stderr)
	require.Equal(t, successKeys, jsonKeys(t, stdout))
	var first success
	require.NoError(t, json.Unmarshal([]byte(stdout), &first))
	p.track(first.PID)
	require.True(t, first.Started)
	require.Equal(t, p.socket, first.Socket)
	require.Equal(t, p.dataDir, first.DataDir)

	stdout, stderr, code = runCrush(t, p.root, os.Environ(), "engine", "ensure", "--cwd", p.cwd)
	require.Equal(t, 0, code, "stderr: %s", stderr)
	var second success
	require.NoError(t, json.Unmarshal([]byte(stdout), &second))
	require.False(t, second.Started, "the second ensure finds the engine")
	require.Equal(t, first.PID, second.PID)

	// Every socket folder candidate is a symlink, so none is safe.
	stdout, stderr, code = runCrush(t, p.root, unsafeSocketEnv(t, p.root), "engine", "ensure", "--cwd", p.cwd)
	require.Equal(t, 6, code, "stderr: %s", stderr)
	require.Equal(t, []string{"error"}, jsonKeys(t, stdout))
	var failure struct {
		Error map[string]string `json:"error"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &failure))
	require.Equal(t, []string{"code", "message"}, slices.Sorted(maps.Keys(failure.Error)))
	require.Equal(t, "unsafe_socket_dir", failure.Error["code"])
	require.Contains(t, failure.Error["message"], "it is a symlink")
}

// TestEngineListShowsLiveEngines: two live engines are listed, and a dead
// engine socket is skipped and left alone.
func TestEngineListShowsLiveEngines(t *testing.T) {
	p := newCmdProject(t)
	otherCwd := filepath.Join(p.root, "q")
	require.NoError(t, os.Mkdir(otherCwd, 0o700))
	otherDataDir, otherSocket := projectEngine(t, otherCwd)

	h1 := p.ensure(t, p.cwd)
	h2 := p.ensure(t, otherCwd)
	require.Equal(t, filepath.Dir(p.socket), filepath.Dir(otherSocket), "one socket folder")

	dead := filepath.Join(filepath.Dir(p.socket), "engine-00000000deadbeef.sock")
	ln, err := net.Listen("unix", dead) //nolint:noctx
	require.NoError(t, err)
	ln.(*net.UnixListener).SetUnlinkOnClose(false)
	require.NoError(t, ln.Close())

	stdout, stderr, code := runCrush(t, p.root, os.Environ(), "engine", "list")
	require.Equal(t, 0, code, "stderr: %s", stderr)
	var engines []proto.EngineInfo
	require.NoError(t, json.Unmarshal([]byte(stdout), &engines), "stdout: %s", stdout)
	require.Len(t, engines, 2)
	byDataDir := map[string]proto.EngineInfo{}
	for _, e := range engines {
		byDataDir[e.DataDir] = e
	}
	require.Equal(t, h1.PID, byDataDir[p.dataDir].PID)
	require.Equal(t, h2.PID, byDataDir[otherDataDir].PID)
	require.Equal(t, proto.ProtocolVersion, byDataDir[p.dataDir].Protocol)
	require.FileExists(t, dead, "the list leaves a dead socket alone")

	// With no engine at all the list is empty, not null.
	require.NoError(t, os.Remove(dead))
	for _, pid := range []int{h1.PID, h2.PID} {
		_ = syscall.Kill(pid, syscall.SIGKILL)
		require.Eventually(t, func() bool { return errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) },
			10*time.Second, 20*time.Millisecond)
	}
	stdout, _, code = runCrush(t, p.root, os.Environ(), "engine", "list")
	require.Equal(t, 0, code)
	require.Equal(t, "[]", strings.TrimSpace(stdout))
}

// TestEngineListReportsUnsafeSocketFolder: with no safe socket folder,
// `crush engine list` fails as ensure does, with unsafe_socket_dir and
// exit 6, instead of listing no engines. `crush server` without --host,
// which looks for its default host only then, reports the same cause.
func TestEngineListReportsUnsafeSocketFolder(t *testing.T) {
	p := newCmdProject(t)
	env := unsafeSocketEnv(t, p.root)

	stdout, stderr, code := runCrush(t, p.root, env, "engine", "list")
	require.Equal(t, 6, code, "stdout: %s\nstderr: %s", stdout, stderr)
	require.Equal(t, []string{"error"}, jsonKeys(t, stdout))
	var failure struct {
		Error map[string]string `json:"error"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &failure))
	require.Equal(t, "unsafe_socket_dir", failure.Error["code"])
	require.Contains(t, failure.Error["message"], "it is a symlink")
	require.Contains(t, stderr, "it is a symlink")

	_, stderr, code = runCrush(t, p.root, env, "server")
	require.NotEqual(t, 0, code)
	require.Contains(t, stderr, "no server host")
	require.Contains(t, stderr, "it is a symlink")
}

// TestEngineVersionTouchesNoSocketFolder: a command that needs no server,
// such as `crush engine version`, which the desktop app runs at start,
// does not look for the socket folder: with an unsafe candidate it prints
// no warning, and it creates no folder.
func TestEngineVersionTouchesNoSocketFolder(t *testing.T) {
	p := newCmdProject(t)
	fresh := filepath.Join(p.root, "v")
	runtimeDir, tmp, target := filepath.Join(fresh, "r"), filepath.Join(fresh, "t"), filepath.Join(fresh, "target")
	for _, dir := range []string{runtimeDir, tmp, target} {
		require.NoError(t, os.MkdirAll(dir, 0o700))
	}
	// The first candidate is a symlink, which the folder check skips with a
	// warning. On Linux the next one, in the temp folder, does not exist
	// yet, and the check would create it.
	unsafe := filepath.Join(runtimeDir, "crush")
	if runtime.GOOS != "linux" {
		unsafe = filepath.Join(tmp, fmt.Sprintf("crush-%d", os.Getuid()))
	}
	require.NoError(t, os.Symlink(target, unsafe))
	runtimeBefore, tmpBefore := dirNames(t, runtimeDir), dirNames(t, tmp)

	env := append(os.Environ(), "XDG_RUNTIME_DIR="+runtimeDir, "TMPDIR="+tmp)
	stdout, stderr, code := runCrush(t, p.root, env, "engine", "version")
	require.Equal(t, 0, code, "stderr: %s", stderr)
	require.Equal(t, []string{"protocol", "version"}, jsonKeys(t, stdout))
	require.Empty(t, stderr)
	require.Equal(t, runtimeBefore, dirNames(t, runtimeDir), "a socket folder was created")
	require.Equal(t, tmpBefore, dirNames(t, tmp), "a socket folder was created")
	require.Empty(t, dirNames(t, target))
}

// setupCmd is a command with the root flags connectToServer and
// setupLocalApp read, parsed from args. --host has no default, as in the
// real command.
func setupCmd(t *testing.T, args ...string) *cobra.Command {
	t.Helper()
	old := clientHost
	t.Cleanup(func() { clientHost = old })
	c := &cobra.Command{Use: "crush"}
	c.SetContext(t.Context())
	c.Flags().StringP("cwd", "c", "", "")
	c.Flags().StringP("data-dir", "D", "", "")
	c.Flags().BoolP("debug", "d", false, "")
	c.Flags().StringVarP(&clientHost, "host", "H", "", "")
	c.Flags().BoolP("yolo", "y", true, "")
	c.Flags().StringSlice("channels", nil, "")
	require.NoError(t, c.ParseFlags(args))
	return c
}

// stopLegacyServer shuts down the legacy server c reaches and waits until
// it let go of its socket and of the project.
func stopLegacyServer(t *testing.T, c *client.Client, socket, dataDir string) {
	t.Helper()
	_ = c.ShutdownServer(context.Background())
	require.Eventually(t, func() bool {
		_, err := os.Stat(socket)
		return errors.Is(err, os.ErrNotExist) && db.ProbeProjectLock(dataDir) == nil
	}, 15*time.Second, 50*time.Millisecond, "the legacy server did not exit")
}

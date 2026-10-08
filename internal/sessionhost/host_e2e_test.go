//go:build !windows

package sessionhost

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
	"github.com/creack/pty"
	"github.com/stretchr/testify/require"
)

// e2eBinaryEnv names a built Crush to run the end-to-end test with. The
// test is skipped without it, since building Crush takes a while.
const e2eBinaryEnv = "CRUSH_HOST_E2E_BINARY"

// screen is a Crush host running in a terminal of its own, as the user's
// terminal would run it.
type screen struct {
	t   *testing.T
	emu *vt.SafeEmulator
	tty *os.File
	cmd *exec.Cmd
	raw *os.File // everything the host wrote, when CRUSH_HOST_E2E_RAW is set
	// project is the test's folder; its state folder holds crash reports.
	project string
}

func startScreen(t *testing.T, binary string, width, height int, extra ...string) *screen {
	t.Helper()
	return startScreenIn(t, t.TempDir(), binary, width, height, extra...)
}

func startScreenIn(t *testing.T, project, binary string, width, height int, extra ...string) *screen {
	t.Helper()
	return startScreenWith(t, project, binary, width, height, nil, extra...)
}

// startScreenWith starts the host in a terminal that setup readies first,
// to answer as a particular terminal would.
func startScreenWith(t *testing.T, project, binary string, width, height int, setup func(*vt.SafeEmulator), extra ...string) *screen {
	t.Helper()
	// Silent and separate from the user's own data: no notifications, a
	// scratch data folder and a scratch state folder for the list setting.
	require.NoError(t, os.WriteFile(filepath.Join(project, "crush.json"),
		[]byte(`{"options":{"notifications":"disabled"}}`), 0o600))
	args := append([]string{"--cwd", project, "--data-dir", filepath.Join(project, ".crush-data"), "--yolo=false"}, extra...)
	cmd := exec.Command(binary, args...)
	cmd.Dir = project
	cmd.Env = append(ChildEnviron(os.Environ(), "CRUSH_CRASH_REPORT", ChildEnv),
		"XDG_STATE_HOME="+filepath.Join(project, "state"))
	// ChildEnviron marks a session; this is the host, so take it out again.
	cmd.Env = withoutVar(cmd.Env, ChildEnv)
	tty, err := pty.StartWithSize(cmd, winsize(width, height))
	require.NoError(t, err)
	emu := vt.NewSafeEmulator(width, height)
	if setup != nil {
		setup(emu)
	}
	var raw *os.File
	if path := os.Getenv("CRUSH_HOST_E2E_RAW"); path != "" {
		raw, err = os.Create(path)
		require.NoError(t, err)
	}
	go func() {
		var out io.Writer = emu
		if raw != nil {
			out = io.MultiWriter(emu, raw)
		}
		_, _ = io.Copy(out, tty)
	}()
	go func() { _, _ = io.Copy(tty, emu) }()
	t.Cleanup(func() {
		_ = cmd.Process.Signal(os.Interrupt)
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		_ = tty.Close()
	})
	return &screen{t: t, emu: emu, tty: tty, cmd: cmd, raw: raw, project: project}
}

func withoutVar(env []string, name string) []string {
	out := env[:0:0]
	for _, kv := range env {
		if !strings.HasPrefix(kv, name+"=") {
			out = append(out, kv)
		}
	}
	return out
}

func (s *screen) text() string {
	buf := uv.NewScreenBuffer(s.emu.Width(), s.emu.Height())
	s.emu.Draw(buf, buf.Bounds())
	lines := strings.Split(strings.ReplaceAll(buf.Render(), "\r\n", "\n"), "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(ansi.Strip(line), " ")
	}
	return strings.Join(lines, "\n")
}

func (s *screen) waitFor(want string, timeout time.Duration) string {
	s.t.Helper()
	deadline := time.Now().Add(timeout)
	var last string
	for time.Now().Before(deadline) {
		last = s.text()
		if strings.Contains(last, want) {
			return last
		}
		time.Sleep(100 * time.Millisecond)
	}
	s.t.Fatalf("screen never showed %q; last screen:\n%s", want, last)
	return ""
}

func (s *screen) press(seq string) {
	_, err := s.tty.Write([]byte(seq))
	require.NoError(s.t, err)
}

func TestHostRunsRealSessions(t *testing.T) {
	binary := os.Getenv(e2eBinaryEnv)
	if binary == "" {
		t.Skipf("set %s to a built Crush to run this test", e2eBinaryEnv)
	}
	s := startScreen(t, binary, 140, 40)

	s.waitFor("Sessions [1]", 20*time.Second)
	// The session drew itself beside the list and reported its state.
	first := s.waitFor("○ ", 30*time.Second)
	t.Logf("Started:\n%s", first)
	time.Sleep(time.Second)
	s.snapshot("1-one-session")

	// Alt+S shrinks the list to the strip and back.
	s.press("\x1bs")
	strip := s.waitFor(" › ", 5*time.Second)
	require.NotContains(t, strip, "alt+n new")
	time.Sleep(500 * time.Millisecond)
	s.snapshot("2-strip")
	s.press("\x1bs")
	s.waitFor("alt+n new", 5*time.Second)

	// A second session in a folder picked in the new-session box, then
	// closing it through the Close/Keep box.
	require.NoError(t, os.Mkdir(filepath.Join(s.project, "subproject"), 0o755))
	click := fmt.Sprintf("\x1b[<0;%d;%dM\x1b[<0;%d;%dm", newSessionColumn+1, newSessionRow+1, newSessionColumn+1, newSessionRow+1)
	s.press(click)
	picker := s.waitFor("New session in…", 5*time.Second)
	require.Contains(t, picker, "subproject")
	time.Sleep(300 * time.Millisecond)
	s.snapshot("3-new-session-box")
	s.press("sub\t")
	s.waitFor("subproject/ ", 5*time.Second)
	s.press("\r")
	s.waitFor("Sessions [2]", 20*time.Second)
	s.waitFor("/subproject", 20*time.Second)
	time.Sleep(2 * time.Second)
	s.snapshot("3-two-sessions")
	s.press("\x1bw")
	box := s.waitFor("Keep", 5*time.Second)
	t.Logf("Close box:\n%s", box)
	// A waiting session adds a sentence, wrapping "saved" to the next row.
	require.Contains(t, box, "The chat stays")
	require.Contains(t, box, "saved and can be reopened later.")
	s.snapshot("4-close-box")
	s.press("\r")
	s.waitFor("Sessions [1]", 20*time.Second)
}

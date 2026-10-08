//go:build !windows

package sessionhost

import (
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
}

func startScreen(t *testing.T, binary string, width, height int) *screen {
	t.Helper()
	project := t.TempDir()
	// Silent and separate from the user's own data: no notifications, a
	// scratch data folder and a scratch state folder for the list setting.
	require.NoError(t, os.WriteFile(filepath.Join(project, "crush.json"),
		[]byte(`{"options":{"notifications":"disabled"}}`), 0o600))
	cmd := exec.Command(binary, "--cwd", project, "--data-dir", filepath.Join(project, ".crush-data"), "--yolo=false")
	cmd.Dir = project
	cmd.Env = append(ChildEnviron(os.Environ(), "CRUSH_CRASH_REPORT", ChildEnv),
		"XDG_STATE_HOME="+filepath.Join(project, "state"))
	// ChildEnviron marks a session; this is the host, so take it out again.
	cmd.Env = withoutVar(cmd.Env, ChildEnv)
	tty, err := pty.StartWithSize(cmd, winsize(width, height))
	require.NoError(t, err)
	emu := vt.NewSafeEmulator(width, height)
	go func() { _, _ = io.Copy(emu, tty) }()
	go func() { _, _ = io.Copy(tty, emu) }()
	t.Cleanup(func() {
		_ = cmd.Process.Signal(os.Interrupt)
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		_ = tty.Close()
	})
	return &screen{t: t, emu: emu, tty: tty}
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

	s.waitFor("Sessions  1", 20*time.Second)
	// The session drew itself beside the list and reported its state.
	first := s.waitFor("Ready", 30*time.Second)
	t.Logf("Started:\n%s", first)
	time.Sleep(time.Second)
	s.snapshot("1-one-session")

	// Alt+S shrinks the list to the strip and back.
	s.press("\x1bs")
	strip := s.waitFor(" › ", 5*time.Second)
	require.NotContains(t, strip, "New session")
	time.Sleep(500 * time.Millisecond)
	s.snapshot("2-strip")
	s.press("\x1bs")
	s.waitFor("New session", 5*time.Second)

	// A second session, then closing it through the Close/Keep box.
	s.press("\x1b[<0;5;2M\x1b[<0;5;2m")
	s.waitFor("Sessions  2", 20*time.Second)
	time.Sleep(2 * time.Second)
	s.snapshot("3-two-sessions")
	s.press("\x1bw")
	box := s.waitFor("Keep", 5*time.Second)
	t.Logf("Close box:\n%s", box)
	require.Contains(t, box, "The chat stays saved")
	s.snapshot("4-close-box")
	s.press("\r")
	s.waitFor("Sessions  1", 20*time.Second)
}

//go:build !windows

package sessionhost

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/charmbracelet/x/vt"
	"github.com/stretchr/testify/require"
)

// openPictureForm starts the host in a terminal readied by setup, in a
// saved chat, and opens a question form there whose choices have pictures
// (when CRUSH_HOST_E2E_PICTURE names one). It returns once the form is
// drawn.
func openPictureForm(t *testing.T, binary string, setup func(*vt.SafeEmulator)) *screen {
	t.Helper()
	project := t.TempDir()
	data := filepath.Join(project, ".crush-data")
	// An AGENTS.md skips the first-run "initialize this project?" prompt.
	require.NoError(t, os.WriteFile(filepath.Join(project, "AGENTS.md"), []byte("# Test\n"), 0o600))
	// Create the database, then a saved chat to open the form in.
	out, err := exec.Command(binary, "session", "list", "--cwd", project, "--data-dir", data).CombinedOutput()
	require.NoError(t, err, string(out))
	now := time.Now().Unix()
	db := filepath.Join(data, "crush.db")
	out, err = exec.Command("sqlite3", db, fmt.Sprintf(
		"INSERT INTO sessions (id, title, updated_at, created_at) VALUES ('form-test', 'Form test', %d, %d);", now, now)).CombinedOutput()
	require.NoError(t, err, string(out))

	started := time.Now()
	s := startScreenWith(t, project, binary, 140, 40, setup, "--session", "form-test")
	s.waitFor("Form test", 30*time.Second)
	time.Sleep(2 * time.Second)

	// The session's Crush made its request folder when it started.
	base := filepath.Join(os.TempDir(), fmt.Sprintf("crush-%d", os.Getuid()))
	dirs, _ := filepath.Glob(filepath.Join(base, "tasks-*"))
	var tasks string
	for _, d := range dirs {
		if fi, err := os.Stat(d); err == nil && fi.ModTime().After(started) {
			tasks = d
		}
	}
	require.NotEmpty(t, tasks, "no request folder from the session")

	picture := os.Getenv("CRUSH_HOST_E2E_PICTURE")
	choice := func(id, label string) string {
		if picture == "" {
			return fmt.Sprintf(`{"id":%q,"label":%q}`, id, label)
		}
		return fmt.Sprintf(`{"id":%q,"label":%q,"image":%q}`, id, label, picture)
	}
	ask := exec.Command(binary, "ask")
	ask.Env = append(os.Environ(), "CRUSH_TASKS_DIR="+tasks, "CRUSH_SESSION_ID=form-test")
	ask.Stdin = strings.NewReader(`{"questions":[{"type":"single_choice","label":"Where","question":"Where should it go?","description":"Test form.","choices":[` +
		choice("a", "Alpha choice") + "," + choice("b", "Beta choice") + `]}]}`)
	out, err = ask.CombinedOutput()
	require.NoError(t, err, string(out))

	form := s.waitFor("Where should it go?", 20*time.Second)
	t.Logf("Form:\n%s", form)
	// Wait for the picture to be drawn, as on a real screen.
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) && strings.Contains(s.text(), "Loading the picture") {
		time.Sleep(200 * time.Millisecond)
	}
	s.snapshot("6-form")
	t.Logf("Picture drawn:\n%s", s.text())
	return s
}

// A question form with picture choices, opened by `crush ask` in a session
// inside the list, must take typing and clicks.
func TestQuestionFormInSessionTakesInput(t *testing.T) {
	binary := os.Getenv(e2eBinaryEnv)
	if binary == "" {
		t.Skipf("set %s to a built Crush to run this test", e2eBinaryEnv)
	}
	s := openPictureForm(t, binary, nil)

	// Move the mouse around over the session, as a hand on the mouse does.
	for i := range 300 {
		s.press(fmt.Sprintf("\x1b[<35;%d;%dM", 40+i%80, 5+i%30))
	}
	// Clicking "Something else?" and typing there must show up quickly.
	x, y := findText(t, s.text(), "Something else?")
	s.press(fmt.Sprintf("\x1b[<0;%d;%dM\x1b[<0;%d;%dm", x+1, y+1, x+1, y+1))
	s.press("zebra")
	typedAt := time.Now()
	for time.Since(typedAt) < 5*time.Second && !strings.Contains(s.text(), "zebra") {
		time.Sleep(200 * time.Millisecond)
	}
	took := time.Since(typedAt)
	t.Logf("typing showed after %s", took.Round(100*time.Millisecond))
	if !strings.Contains(s.text(), "zebra") || took > 2*time.Second {
		s.snapshot("7-stuck")
		dumpSessionStacks(t, s.cmd.Process.Pid)
		reports, _ := filepath.Glob(filepath.Join(s.project, "state", "crush", "crashes", "*.log"))
		for _, r := range reports {
			if b, err := os.ReadFile(r); err == nil {
				t.Logf("session stacks:\n%s", b)
			}
		}
		t.Fatalf("typing took %s to reach the form; screen:\n%s", took.Round(100*time.Millisecond), s.text())
	}
	s.snapshot("7-typed")
}

// The first-run "initialize this project?" prompt must take keys inside
// the list: Right moves to Nope, Enter picks it and the prompt closes.
func TestInitPromptInSessionTakesKeys(t *testing.T) {
	binary := os.Getenv(e2eBinaryEnv)
	if binary == "" {
		t.Skipf("set %s to a built Crush to run this test", e2eBinaryEnv)
	}
	s := startScreen(t, binary, 140, 40)
	s.waitFor("Would you like to initialize now?", 30*time.Second)
	time.Sleep(time.Second)
	s.press("\x1b[C")
	time.Sleep(500 * time.Millisecond)
	s.press("\r")
	time.Sleep(3 * time.Second)
	text := s.text()
	if strings.Contains(text, "Would you like to initialize now?") {
		if s.raw != nil {
			out, _ := exec.Command("pgrep", "-P", strconv.Itoa(s.cmd.Process.Pid)).Output()
			for _, pid := range strings.Fields(string(out)) {
				n, _ := strconv.Atoi(pid)
				_ = syscall.Kill(n, syscall.SIGQUIT)
			}
			time.Sleep(2 * time.Second)
		}
		t.Fatalf("the prompt ignored the keys; screen:\n%s", text)
	}
	t.Logf("After Nope:\n%s", text)
}

// dumpSessionStacks has every session Crush under the host print where its
// goroutines are; their crash guards save that under the state folder.
func dumpSessionStacks(t *testing.T, host int) {
	t.Helper()
	var walk func(pid int)
	walk = func(pid int) {
		out, _ := exec.Command("pgrep", "-P", strconv.Itoa(pid)).Output()
		for _, field := range strings.Fields(string(out)) {
			child, _ := strconv.Atoi(field)
			env, _ := os.ReadFile(fmt.Sprintf("/proc/%d/environ", child))
			if strings.Contains(string(env), ChildEnv+"=1") && strings.Contains(string(env), "CRUSH_CRASH_REPORT=") {
				_ = syscall.Kill(child, syscall.SIGQUIT)
			}
			walk(child)
		}
	}
	walk(host)
	time.Sleep(3 * time.Second)
}

// findText returns the column and row where want first shows on screen.
func findText(t *testing.T, screen, want string) (x, y int) {
	t.Helper()
	for row, line := range strings.Split(screen, "\n") {
		if i := strings.Index(line, want); i >= 0 {
			return len([]rune(line[:i])), row
		}
	}
	t.Fatalf("%q is not on screen:\n%s", want, screen)
	return 0, 0
}

// In a terminal that shows Kitty graphics, the question form's picture
// reaches the real terminal, put in the session's part of the screen, past
// the list, instead of being drawn with characters.
func TestPicturesInSessionReachTheRealTerminal(t *testing.T) {
	binary := os.Getenv(e2eBinaryEnv)
	if binary == "" || os.Getenv("CRUSH_HOST_E2E_PICTURE") == "" {
		t.Skipf("set %s and CRUSH_HOST_E2E_PICTURE to run this test", e2eBinaryEnv)
	}
	var mu sync.Mutex
	var placedAt []int // the column each picture was put at
	kittyTerminal := func(emu *vt.SafeEmulator) {
		emu.RegisterApcHandler(func(data []byte) bool {
			if !strings.HasPrefix(string(data), "G") {
				return false
			}
			if strings.Contains(string(data), "a=q") {
				go func() { _, _ = io.WriteString(emu.InputPipe(), "\x1b_Gi=31;OK\x1b\\") }()
				return true
			}
			if strings.Contains(string(data), "a=T") {
				mu.Lock()
				placedAt = append(placedAt, emu.Emulator.CursorPosition().X)
				mu.Unlock()
			}
			return true
		})
	}
	s := openPictureForm(t, binary, kittyTerminal)
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(placedAt)
		mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	require.NotEmpty(t, placedAt, "no picture reached the real terminal; screen:\n%s", s.text())
	require.GreaterOrEqual(t, placedAt[0], listWidth, "put past the list")
	require.NotContains(t, s.text(), "Loading the picture")
}

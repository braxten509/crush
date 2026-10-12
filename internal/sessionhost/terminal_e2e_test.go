//go:build !windows

package sessionhost

import (
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/charmbracelet/x/vt"
	"github.com/stretchr/testify/require"
)

func TestHostRunsNormalTerminalTab(t *testing.T) {
	binary := os.Getenv(e2eBinaryEnv)
	if binary == "" {
		t.Skipf("set %s to a built Crush to run this test", e2eBinaryEnv)
	}
	t.Setenv("SHELL", "/bin/sh")
	t.Setenv("ENV", "")
	t.Setenv("PS1", "TERM> ")
	s := startScreen(t, binary, 140, 40)
	s.waitFor("Sessions [1]", 20*time.Second)
	s.waitFor("○ ", 30*time.Second)
	dir := filepath.Join(s.project, "chosen folder")
	require.NoError(t, os.Mkdir(dir, 0o700))
	x, y := newTerminalColumn+1, newSessionRow+1
	s.press(fmt.Sprintf("\x1b[<0;%d;%dM\x1b[<0;%d;%dm", x, y, x, y))
	s.waitFor("New terminal in…", 5*time.Second)
	s.snapshot("terminal-1-folder-chooser")
	s.press("chosen folder\r")
	s.waitFor("Terminals [1]", 10*time.Second)
	s.waitFor("TERM>", 10*time.Second)
	s.press("printf 'HERE:%s\\n' \"$PWD\"\r")
	s.waitFor("HERE:"+dir, 5*time.Second)
	s.snapshot("terminal-2-shell")
	s.press("value=kept\r\x1b1\x1b2")
	s.press("printf 'VALUE:%s\\n' \"$value\"\r")
	s.waitFor("VALUE:kept", 5*time.Second)
	s.press("\x1bs")
	s.waitFor(" › ", 5*time.Second)
	s.snapshot("terminal-3-strip")
	s.press("\x1bs\x1bw")
	s.waitFor("Its output is not", 5*time.Second)
	s.snapshot("terminal-4-close")
	s.press("\x1b")
	s.waitFor("VALUE:kept", 5*time.Second)
	s.press("exit\r")
	s.waitFor("Sessions [1]", 10*time.Second)
}

func TestTerminalKeepsOriginalBackgroundWhenCrushColorIsOff(t *testing.T) {
	binary := os.Getenv(e2eBinaryEnv)
	if binary == "" {
		t.Skipf("set %s to a built Crush to run this test", e2eBinaryEnv)
	}
	configDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "crush.json"), []byte(`{"options":{"notifications":"disabled","tui":{"active_theme":"graphite","transparent":true}}}`), 0o600))
	t.Setenv("CRUSH_GLOBAL_CONFIG", configDir)
	t.Setenv("SHELL", "/bin/sh")
	t.Setenv("ENV", "")
	t.Setenv("PS1", "TERM> ")
	background := color.RGBA{R: 0x13, G: 0x23, B: 0x33, A: 255}
	s := startScreenWith(t, t.TempDir(), binary, 140, 40, func(emu *vt.SafeEmulator) {
		emu.SetDefaultBackgroundColor(background)
	})
	s.waitFor("○ ", 30*time.Second)
	s.press("\x1bt")
	s.waitFor("New terminal in…", 5*time.Second)
	s.press("\r")
	s.waitFor("TERM>", 10*time.Second)
	require.Equal(t, background, color.RGBAModel.Convert(s.emu.BackgroundColor()))
	cell := s.emu.CellAt(listWidth, 0)
	require.NotNil(t, cell)
	require.Equal(t, background, color.RGBAModel.Convert(cell.Style.Bg))
	s.snapshot("terminal-5-original-background")
}

func TestNewTabsKeepTheThemeWhileStarting(t *testing.T) {
	binary := os.Getenv(e2eBinaryEnv)
	if binary == "" {
		t.Skipf("set %s to a built Crush to run this test", e2eBinaryEnv)
	}
	configDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "crush.json"), []byte(`{"options":{"notifications":"disabled","tui":{"active_theme":"graphite"}}}`), 0o600))
	t.Setenv("CRUSH_GLOBAL_CONFIG", configDir)
	t.Setenv("SHELL", "/bin/sh")
	t.Setenv("ENV", "")
	t.Setenv("PS1", "TERM> ")
	s := startScreen(t, binary, 140, 40)
	s.waitFor("○ ", 30*time.Second)
	time.Sleep(time.Second)
	want := s.emu.BackgroundColor()
	steady := func(what string) {
		for start := time.Now(); time.Since(start) < 4*time.Second; time.Sleep(10 * time.Millisecond) {
			require.Equal(t, want, s.emu.BackgroundColor(), "%s changed the theme after %s", what, time.Since(start))
		}
	}
	s.press("\x1bt")
	s.waitFor("New terminal in…", 5*time.Second)
	s.press("\r")
	steady("a new terminal")
	s.press("\x1bn")
	s.waitFor("New session in…", 5*time.Second)
	s.press("\r")
	steady("a new chat")
}

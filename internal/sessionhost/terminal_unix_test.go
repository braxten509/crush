//go:build !windows

package sessionhost

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func interactiveTerminalHost(t *testing.T) (*Host, <-chan tea.Msg) {
	t.Helper()
	h := newTestHost(t, 120, 30, Status{Title: "Chat", Dir: t.TempDir(), Theme: "graphite", State: StateReady})
	h.started = true
	h.opts.Shell = "/bin/sh"
	h.opts.ShellEnv = append(TerminalEnviron(os.Environ(), "ENV", "PS1"), "ENV=", "PS1=TERM> ")
	events := make(chan tea.Msg, 128)
	h.SetSend(func(msg tea.Msg) {
		select {
		case events <- msg:
		case <-t.Context().Done():
		}
	})
	t.Cleanup(func() {
		for _, s := range h.sessions {
			if s.cmd != nil {
				s.stop()
				require.Eventually(t, s.exited.Load, 6*time.Second, 20*time.Millisecond)
			}
		}
	})
	return h, events
}

func requireTerminalOutput(t *testing.T, s *session, text string) {
	t.Helper()
	requireTerminalOutputWithin(t, s, text, 5*time.Second)
}

func requireTerminalOutputWithin(t *testing.T, s *session, text string, timeout time.Duration) {
	t.Helper()
	var last string
	matched := assert.Eventually(t, func() bool {
		output := ansi.Strip(s.emu.Render())
		last = output
		// A normal terminal wraps long paths onto its next screen row.
		output = strings.NewReplacer("\r", "", "\n", "").Replace(output)
		return strings.Contains(output, text)
	}, timeout, 20*time.Millisecond, "terminal output should contain %q", text)
	if !matched {
		length, total := s.emu.ScrollbackState()
		t.Fatalf("History: %d retained / %d written. Last screen:\n%s", length, total, last)
	}
}

func requireShellForeground(t *testing.T, s *session) {
	t.Helper()
	require.Eventually(t, func() bool {
		foreground, err := unix.IoctlGetInt(int(s.tty.Fd()), unix.TIOCGPGRP)
		return err == nil && foreground == s.cmd.Process.Pid
	}, 5*time.Second, 20*time.Millisecond, "shell must regain the terminal after Ctrl+C")
}

func TestTerminalPickerStartsARealShellInChosenFolder(t *testing.T) {
	h, events := interactiveTerminalHost(t)
	dir := filepath.Join(t.TempDir(), "chosen folder")
	require.NoError(t, os.Mkdir(dir, 0o700))
	press(h, "alt+t")
	h.picker.setInput(dir)
	press(h, "enter")
	require.Nil(t, h.picker)
	require.Len(t, h.sessions, 2)
	s := h.current()
	require.Equal(t, terminalSession, s.kind)
	require.NotEqual(t, h.sessions[0].id, s.id)
	require.Equal(t, dir, s.cmd.Dir)
	requireTerminalOutput(t, s, "TERM> ")
	h.Update(tea.PasteMsg{Content: "printf 'HERE:%s\\n' \"$PWD\""})
	key(h, tea.KeyEnter)
	requireTerminalOutput(t, s, "HERE:"+dir)

	// Switching does not recreate the shell or lose its variables.
	s.emu.SendText("value=preserved\n")
	process := s.cmd.Process.Pid
	press(h, "alt+1")
	press(h, "alt+2")
	require.Equal(t, process, h.current().cmd.Process.Pid)
	s.emu.SendText("printf 'VALUE:%s\\n' \"$value\"\n")
	requireTerminalOutput(t, s, "VALUE:preserved")

	h.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	s.emu.SendText("stty size\n")
	requireTerminalOutput(t, s, "40 110")

	// Control-D ends the shell and removes just its own tab.
	h.Update(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
	require.Eventually(t, s.exited.Load, 5*time.Second, 20*time.Millisecond)
	require.Eventually(t, func() bool {
		select {
		case msg := <-events:
			h.Update(msg)
		default:
		}
		return len(h.sessions) == 1
	}, 5*time.Second, 20*time.Millisecond)
	require.Equal(t, "Chat", h.current().snapshot().Title)
}

func TestTerminalControlCStopsTheForegroundCommand(t *testing.T) {
	h, _ := interactiveTerminalHost(t)
	require.NoError(t, h.startTerminal(t.TempDir()))
	s := h.current()
	requireTerminalOutput(t, s, "TERM> ")
	s.emu.SendText("sleep 30\n")
	require.Eventually(t, func() bool {
		foreground, err := unix.IoctlGetInt(int(s.tty.Fd()), unix.TIOCGPGRP)
		return err == nil && foreground != s.cmd.Process.Pid
	}, 5*time.Second, 20*time.Millisecond)
	h.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	requireShellForeground(t, s)
	s.emu.SendText("printf 'CONTROL:%s\\n' returned\n")
	requireTerminalOutput(t, s, "CONTROL:returned")
	require.False(t, s.exited.Load())
}

func TestTerminalLargePasteDoesNotBlockTheHost(t *testing.T) {
	h, _ := interactiveTerminalHost(t)
	require.NoError(t, h.startTerminal(t.TempDir()))
	s := h.current()
	requireTerminalOutput(t, s, "TERM> ")
	s.emu.SendText("stty -echo; cat\n")
	require.Eventually(t, func() bool {
		foreground, err := unix.IoctlGetInt(int(s.tty.Fd()), unix.TIOCGPGRP)
		return err == nil && foreground != s.cmd.Process.Pid
	}, 5*time.Second, 20*time.Millisecond)
	returned := make(chan struct{})
	go func() {
		h.Update(tea.PasteMsg{Content: strings.Repeat("paste line\n", 16*1024) + "PASTE_COMPLETE\n"})
		close(returned)
	}()
	select {
	case <-returned:
	case <-time.After(time.Second):
		t.Fatal("a paste blocked the host instead of queuing terminal input")
	}
	press(h, "alt+1")
	require.Equal(t, 0, h.active)
	press(h, "alt+2")
	require.Same(t, s, h.current())
	// Race instrumentation and other package checks can slow the full echo;
	// host responsiveness is checked separately above with a one-second bound.
	requireTerminalOutputWithin(t, s, "PASTE_COMPLETE", 15*time.Second)
	h.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	requireShellForeground(t, s)
	s.emu.SendText("printf 'AFTER:%s\\n' paste\n")
	requireTerminalOutput(t, s, "AFTER:paste")
}

func TestTerminalCloseHangsUpForegroundAndStoppedJobs(t *testing.T) {
	for _, stopped := range []bool{false, true} {
		t.Run(map[bool]string{false: "foreground", true: "stopped"}[stopped], func(t *testing.T) {
			h, _ := interactiveTerminalHost(t)
			require.NoError(t, h.startTerminal(t.TempDir()))
			s := h.current()
			requireTerminalOutput(t, s, "TERM> ")
			s.emu.SendText("sleep 30\n")
			var job int
			require.Eventually(t, func() bool {
				foreground, err := unix.IoctlGetInt(int(s.tty.Fd()), unix.TIOCGPGRP)
				if err == nil && foreground != s.cmd.Process.Pid {
					job = foreground
				}
				return job > 0
			}, 5*time.Second, 20*time.Millisecond)
			t.Cleanup(func() { _ = unix.Kill(-job, unix.SIGKILL) })
			if stopped {
				h.Update(tea.KeyPressMsg{Code: 'z', Mod: tea.ModCtrl})
				require.Eventually(t, func() bool {
					foreground, err := unix.IoctlGetInt(int(s.tty.Fd()), unix.TIOCGPGRP)
					return err == nil && foreground == s.cmd.Process.Pid
				}, 5*time.Second, 20*time.Millisecond)
			}
			s.stop()
			require.Eventually(t, s.exited.Load, time.Second, 20*time.Millisecond)
			require.Eventually(t, func() bool {
				return unix.Kill(-job, 0) != nil
			}, 5*time.Second, 20*time.Millisecond, "normal foreground/stopped jobs should receive terminal hangup")
		})
	}
}

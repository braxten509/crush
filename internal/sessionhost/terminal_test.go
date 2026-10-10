package sessionhost

import (
	"image/color"
	"io"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

func TestTerminalActionUsesTheFolderPicker(t *testing.T) {
	root := folders(t, []string{"alpha", "with spaces"})
	h, started := pickerHost(t, root)
	h.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: newTerminalColumn, Y: newSessionRow})
	require.NotNil(t, h.picker)
	require.Equal(t, terminalSession, h.picker.kind)
	require.Contains(t, screenText(h), "New terminal in…")
	require.Equal(t, root, h.picker.base)
	clearInput(h)
	typeText(h, "with spaces")
	dir, ok := h.picker.choice()
	require.True(t, ok)
	require.Equal(t, filepath.Join(root, "with spaces"), dir)
	press(h, "esc")
	require.Nil(t, h.picker)
	require.Empty(t, *started)

	press(h, "alt+t")
	require.Equal(t, terminalSession, h.picker.kind)
	press(h, "esc")
	press(h, "alt+n")
	require.Equal(t, chatSession, h.picker.kind, "New session still opens a chat")
}

func TestTerminalActionDoesNotOverlapSessionRows(t *testing.T) {
	for _, width := range []int{60, 120} {
		for _, height := range []int{3, 7, 9, 12, 30} {
			h := newTestHost(t, width, height, Status{Title: "One"}, Status{Title: "Two"}, Status{Title: "Three"})
			for i := range h.sessions {
				h.show(i)
				for y := height - sideFooterRows; y < height; y++ {
					require.Equal(t, -1, h.rowAt(2, y).session, "footer rows cannot select a session")
				}
				require.NotPanics(t, func() { h.View() })
			}
			if h.listOpen() {
				h.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: newTerminalColumn, Y: newSessionRow})
				require.NotNil(t, h.picker)
				require.Equal(t, terminalSession, h.picker.kind)
			} else {
				h.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 1, Y: stripTerminalRow})
				require.NotNil(t, h.picker)
				require.Equal(t, terminalSession, h.picker.kind)
			}
		}
	}
}

func TestTerminalEnvironPreservesUserSettingsAndGuards(t *testing.T) {
	env := TerminalEnviron([]string{
		"PATH=/normal/path", "SHELL=/bin/zsh", "CUSTOM_SETTING=keep", "CODEX_GIT_GUARD=keep",
		"CRUSH_TASKS_DIR=/chat/tasks", "CRUSH_SESSION_ID=chat", ChildEnv + "=1", "CRUSH_CRASH_REPORT=old",
		"TERM=konsole", "COLORTERM=old", "TERM_PROGRAM=Konsole", "KONSOLE_VERSION=250800",
	}, "CRUSH_CRASH_REPORT")
	for _, item := range []string{"PATH=/normal/path", "SHELL=/bin/zsh", "CUSTOM_SETTING=keep", "CODEX_GIT_GUARD=keep", "TERM=xterm-256color", "COLORTERM=truecolor"} {
		require.Contains(t, env, item)
	}
	for _, item := range env {
		require.False(t, strings.HasPrefix(item, "CRUSH_") || strings.HasPrefix(item, "KONSOLE_") || strings.HasPrefix(item, "TERM_PROGRAM="))
	}
}

func TestTerminalUsesChatAppearanceAndBackgroundSetting(t *testing.T) {
	h := newTestHost(t, 120, 30, Status{Title: "Chat", Theme: "graphite"}, Status{Title: "Terminal"})
	s := h.sessions[1]
	s.kind = terminalSession
	h.bg = lipgloss.Color("#132333")
	h.colorsKnown = true
	h.refreshTerminalColors()
	s.emu.Write([]byte("default\x1b[48;2;200;10;20mX\x1b[0m"))
	h.show(1)
	require.Equal(t, "graphite", h.theme)
	require.Equal(t, h.styles.Background, s.emu.BackgroundColor())

	h.show(0)
	h.sessions[0].status.UseTerminalBackground = true
	h.statusChanged(1)
	h.show(1)
	require.Equal(t, h.bg, h.View().BackgroundColor)
	require.Equal(t, h.bg, s.emu.BackgroundColor())

	// A program can set its own default, then reset to the current theme.
	s.emu.Write([]byte("\x1b]11;#abcdef\x07"))
	h.show(0)
	h.sessions[0].status.UseTerminalBackground = false
	h.statusChanged(1)
	require.Equal(t, color.RGBA{R: 0xab, G: 0xcd, B: 0xef, A: 255}, color.RGBAModel.Convert(s.emu.BackgroundColor()))
	s.emu.Write([]byte("\x1b]111\x07"))
	require.Equal(t, h.styles.Background, s.emu.BackgroundColor())
	h.show(1)
	require.Equal(t, h.styles.Background, h.View().BackgroundColor)
	require.Equal(t, color.RGBA{R: 200, G: 10, B: 20, A: 255}, color.RGBAModel.Convert(s.emu.CellAt(7, 0).Style.Bg))
}

func TestHostCapturesOriginalBackgroundBeforeReplacingIt(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	h := New(Options{})
	h.width, h.height = 120, 30
	require.Nil(t, h.View().BackgroundColor)
	h.bg = lipgloss.Color("#132333")
	h.colorsKnown = true
	h.applyAppearance(Status{Theme: "graphite", UseTerminalBackground: true})
	require.Equal(t, h.bg, h.View().BackgroundColor)
}

func TestTerminalFollowsLateAppearanceFromItsLastChat(t *testing.T) {
	h := newTestHost(t, 120, 30, Status{Title: "Starting chat"}, Status{Title: "Other chat"}, Status{Title: "Terminal"})
	h.sessions[2].kind = terminalSession
	h.show(2)
	h.sessions[0].status.Theme = "graphite"
	h.sessions[0].status.UseTerminalBackground = true
	h.statusChanged(h.sessions[0].id)
	require.Equal(t, "graphite", h.theme, "opening a terminal early must not lose its chat's theme")
	require.True(t, h.useTerminalBackground)
	h.sessions[1].status.Theme = "other"
	h.statusChanged(h.sessions[1].id)
	require.Equal(t, "graphite", h.theme, "an unrelated hidden chat must not recolor the terminal")
}

func TestTerminalIgnoresPrivateChatStatus(t *testing.T) {
	s := newSession(1, 40, 10, nil, nil)
	s.kind = terminalSession
	s.status = Status{Title: "Terminal", Dir: "/chosen/folder", State: StateReady}
	want := s.snapshot()
	s.emu.Write([]byte(Status{Title: "Wrong", Dir: "/wrong", Theme: "other", State: StateWorking}.Sequence()))
	require.Equal(t, want, s.snapshot())
	require.False(t, s.changed.Load())
	s.emu.Write([]byte("\x1b]7;file://localhost/chosen/next%20folder\x07"))
	require.Equal(t, "/chosen/next folder", s.snapshot().Dir)
	s.emu.Write([]byte("\x1b]7;file://remote.example/remote\x07"))
	require.Equal(t, "/chosen/next folder", s.snapshot().Dir)
}

func TestTerminalScrollbackAndMouseReporting(t *testing.T) {
	h := newTestHost(t, 120, 30, Status{Title: "Terminal"})
	s := h.current()
	go func() { _, _ = io.Copy(io.Discard, s.emu) }()
	t.Cleanup(func() { _ = s.emu.Close() })
	s.kind = terminalSession
	for _, line := range []string{"one", "two", "three", "four", "five", "six", "seven"} {
		s.emu.Write([]byte(line + "\r\n"))
	}
	s.refreshScrollback()
	require.True(t, s.terminalMouse(tea.MouseWheelMsg{Button: tea.MouseWheelUp}))
	require.Positive(t, s.scrollOffset)
	require.Contains(t, ansi.Strip(h.View().Content), "one")
	require.Nil(t, h.View().Cursor)
	h.Update(tea.PasteMsg{Content: "echo hello"})
	require.Zero(t, s.scrollOffset, "paste returns to the live prompt")
	key(h, 'x')
	require.Zero(t, s.scrollOffset)

	s.emu.Write([]byte("\x1b[?1000h\x1b[?1006h"))
	require.False(t, s.terminalMouse(tea.MouseWheelMsg{Button: tea.MouseWheelUp}), "requested mouse events go to the app")
	s.emu.Write([]byte("\x1b[?1000l"))
	require.True(t, s.terminalMouse(tea.MouseWheelMsg{Button: tea.MouseWheelUp}))
	page := tea.KeyPressMsg{Code: tea.KeyPgDown, Mod: tea.ModShift}
	require.True(t, s.terminalKey(page))
	require.Zero(t, s.scrollOffset)

	s.emu.Write([]byte("\x1b[?1049hAPP"))
	s.refreshScrollback()
	require.Zero(t, s.scrollOffset)
	require.False(t, s.terminalKey(page), "full-screen apps keep their own keyboard controls")
	require.NotContains(t, ansi.Strip(h.View().Content), "one")
}

func TestTerminalCloseKeepsChatCopyOut(t *testing.T) {
	h := newTestHost(t, 120, 30, Status{Title: "Terminal"})
	h.current().kind = terminalSession
	press(h, "alt+w")
	text := screenText(h)
	require.Contains(t, text, "Its output is not")
	require.Contains(t, text, "saved.")
	require.NotContains(t, text, "chat stays saved")
	press(h, "esc")
	require.False(t, h.current().stopped)
}

func TestTerminalHistoryStaysPutWhenTheBufferIsFull(t *testing.T) {
	h := newTestHost(t, 120, 30, Status{Title: "Terminal"})
	s := h.current()
	s.kind = terminalSession
	s.emu.Resize(20, 2)
	s.emu.SetScrollbackSize(3)
	s.emu.Write([]byte("one\r\ntwo\r\nthree\r\nfour\r\nfive"))
	s.scrollTerminal(2)
	view := func() string {
		scr := uv.NewScreenBuffer(20, 2)
		s.emu.DrawViewport(scr, scr.Bounds(), s.scrollOffset)
		return ansi.Strip(scr.Render())
	}
	before := view()
	require.Contains(t, before, "two")
	s.emu.Write([]byte("\r\nsix"))
	h.Update(outputMsg{id: s.id})
	require.Equal(t, before, view(), "new output must not move history under the reader")
	s.emu.Write([]byte("\r\nseven"))
	h.Update(outputMsg{id: s.id})
	require.Contains(t, view(), "three", "evicted content clamps to the oldest remaining row")
	s.emu.Write([]byte("\x1b[3J"))
	h.Update(outputMsg{id: s.id})
	require.Zero(t, s.scrollOffset, "cleared history returns to the live screen")
}

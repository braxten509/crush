package sessionhost

import (
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strconv"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
)

type sessionKind uint8

const (
	chatSession sessionKind = iota
	terminalSession
)

func (h *Host) newTerminal(dir string) {
	h.notice = ""
	if err := h.startTerminal(dir); err != nil {
		slog.Error("Could not start a terminal", "error", err)
		h.notice = "Couldn't start the terminal."
	}
}

func (h *Host) startTerminal(dir string) error {
	shell := h.opts.Shell
	if shell == "" {
		shell = os.Getenv("SHELL")
	}
	if shell == "" {
		shell = "/bin/sh"
	}
	env := h.opts.ShellEnv
	if env == nil {
		env = TerminalEnviron(os.Environ())
	}
	w, ht := h.sessionSize()
	h.nextID++
	p := h.palette()
	l := launch{exe: shell, args: []string{"-i"}, dir: dir, env: env}
	s, err := startSession(h.nextID, l, w, ht, p.text, p.bg, h.send, func(s *session) {
		s.kind = terminalSession
		s.status = Status{Title: "Terminal", Dir: dir, State: StateReady}
		s.kitty.Store(h.kitty)
		h.shareCellSize(s)
	})
	if err != nil {
		return fmt.Errorf("open %s: %w", shell, err)
	}
	h.sessions = append(h.sessions, s)
	h.show(len(h.sessions) - 1)
	return nil
}

// applyAppearance follows the shown chat. Shell tabs keep that appearance
// rather than replacing it with an empty chat status of their own.
func (h *Host) applyAppearance(st Status) {
	changed := h.useTerminalBackground != st.UseTerminalBackground
	h.useTerminalBackground = st.UseTerminalBackground
	h.applyTheme(st.Theme)
	if changed {
		h.refreshTerminalColors()
	}
}

func (h *Host) refreshTerminalColors() {
	p := h.palette()
	for _, s := range h.sessions {
		if s.kind == terminalSession {
			s.emu.SetDefaultForegroundColor(p.text)
			s.emu.SetDefaultBackgroundColor(p.bg)
		}
	}
}

func (s *session) closeDetail() string {
	if s.kind == terminalSession {
		return "This closes the terminal. Its output is not saved."
	}
	return confirmDetail(s.snapshot())
}

// workingDir also checks a local shell's current folder, for shells that
// don't report directory changes with OSC 7.
func (s *session) workingDir() string {
	if s.kind == terminalSession && s.cmd != nil && s.cmd.Process != nil {
		if dir, err := os.Readlink(filepath.Join("/proc", strconv.Itoa(s.cmd.Process.Pid), "cwd")); err == nil {
			return dir
		}
	}
	return s.snapshot().Dir
}

// setWorkingDir runs under the emulator lock. It never calls the emulator.
func (s *session) setWorkingDir(value string) {
	if s.kind != terminalSession {
		return
	}
	u, err := url.Parse(value)
	if err != nil || u.Scheme != "file" || !filepath.IsAbs(u.Path) {
		return
	}
	hostname, _ := os.Hostname()
	if u.Host != "" && u.Host != "localhost" && u.Host != hostname {
		return
	}
	s.mu.Lock()
	s.status.Dir = u.Path
	s.mu.Unlock()
	s.changed.Store(true)
}

func (s *session) refreshScrollback() {
	if s.kind != terminalSession {
		return
	}
	n, total := s.emu.ScrollbackState()
	if s.scrollOffset > 0 && total > s.scrollbackTotal {
		s.scrollOffset += int(min(total-s.scrollbackTotal, uint64(n)))
	}
	s.scrollOffset = min(s.scrollOffset, n)
	s.scrollbackLen = n
	s.scrollbackTotal = total
	if s.emu.IsAltScreen() {
		s.scrollOffset = 0
	}
}

func (s *session) scrollTerminal(lines int) {
	s.refreshScrollback()
	s.scrollOffset = min(max(s.scrollOffset+lines, 0), s.scrollbackLen)
}

func (s *session) terminalKey(msg tea.KeyPressMsg) bool {
	if s.kind != terminalSession || s.emu.IsAltScreen() {
		return false
	}
	key := msg.Key()
	if key.Mod != tea.ModShift {
		return false
	}
	switch key.Code {
	case tea.KeyPgUp:
		s.scrollTerminal(max(s.emu.Height()-1, 1))
	case tea.KeyPgDown:
		s.scrollTerminal(-max(s.emu.Height()-1, 1))
	default:
		return false
	}
	return true
}

func (s *session) terminalMouse(msg tea.MouseMsg) bool {
	if s.kind != terminalSession || s.emu.MouseReporting() {
		return false
	}
	wheel, ok := msg.(tea.MouseWheelMsg)
	if !ok {
		return false
	}
	var lines int
	switch wheel.Button {
	case tea.MouseWheelUp:
		lines = 3
	case tea.MouseWheelDown:
		lines = -3
	default:
		return false
	}
	if s.emu.IsAltScreen() {
		key := uv.KeyUp
		if lines < 0 {
			key = uv.KeyDown
		}
		for range 3 {
			s.emu.SendKey(uv.KeyPressEvent{Code: key})
		}
	} else {
		s.scrollTerminal(lines)
	}
	return true
}

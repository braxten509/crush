package sessionhost

import (
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/clipboard"
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
	h.addSession(s)
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

// textPos is a cell of a terminal's text. line counts every line the
// terminal ever had, history included, so a mark stays on its text while
// new output pushes lines into history.
type textPos struct{ line, col int }

func (a textPos) before(b textPos) bool {
	return a.line < b.line || (a.line == b.line && a.col < b.col)
}

// history returns how many lines of history the terminal keeps, and how
// many lines went before the oldest of them. A full-screen app's screen
// has none.
func (s *session) history() (kept, dropped int) {
	if s.emu.IsAltScreen() {
		return 0, 0
	}
	n, total := s.emu.ScrollbackState()
	return n, int(total) - n
}

// textAt returns the text position under the cell x, y of the shown view,
// kept inside it.
func (s *session) textAt(x, y int) textPos {
	kept, dropped := s.history()
	x = min(max(x, 0), s.emu.Width()-1)
	y = min(max(y, 0), s.emu.Height()-1)
	return textPos{line: dropped + kept - min(s.scrollOffset, kept) + y, col: x}
}

// cellAt returns the cell at col of a text line, or nil when that line is
// no longer kept.
func (s *session) cellAt(col, line int) *uv.Cell {
	kept, dropped := s.history()
	row := line - dropped
	switch {
	case row < 0:
		return nil
	case row < kept:
		return s.emu.ScrollbackCellAt(col, row)
	case row-kept < s.emu.Height():
		return s.emu.CellAt(col, row-kept)
	}
	return nil
}

// selection returns the marked text's first and last cells, in order.
func (s *session) selection() (start, end textPos, ok bool) {
	if !s.selected || s.selAlt != s.emu.IsAltScreen() {
		return start, end, false
	}
	start, end = s.selAnchor, s.selHead
	if end.before(start) {
		start, end = end, start
	}
	return start, end, true
}

func (s *session) clearSelection() {
	s.selecting, s.selected = false, false
}

// selectedText returns the marked text, each line without trailing spaces.
func (s *session) selectedText() string {
	start, end, ok := s.selection()
	if !ok {
		return ""
	}
	var lines []string
	for line := start.line; line <= end.line; line++ {
		first, last := 0, s.emu.Width()-1
		if line == start.line {
			first = start.col
		}
		if line == end.line {
			last = end.col
		}
		var b strings.Builder
		for col := first; col <= last; {
			cell := s.cellAt(col, line)
			if cell == nil || cell.Content == "" {
				b.WriteByte(' ')
				col++
				continue
			}
			b.WriteString(cell.Content)
			col += max(cell.Width, 1)
		}
		lines = append(lines, strings.TrimRight(b.String(), " "))
	}
	return strings.Join(lines, "\n")
}

// terminalSelect marks text in a terminal as the left button drags over
// it, and copies it on release. A program that asked for the mouse gets
// it, unless Shift is held. ok reports that the event was used.
func (s *session) terminalSelect(msg tea.MouseMsg, x, y int) (cmd tea.Cmd, ok bool) {
	if s.kind != terminalSession {
		return nil, false
	}
	m := msg.Mouse()
	switch msg.(type) {
	case tea.MouseClickMsg:
		s.clearSelection()
		if m.Button != tea.MouseLeft || (s.emu.MouseReporting() && m.Mod&tea.ModShift == 0) {
			return nil, false
		}
		s.selecting, s.selAlt = true, s.emu.IsAltScreen()
		s.selAnchor = s.textAt(x, y)
		s.selHead = s.selAnchor
		return nil, true
	case tea.MouseMotionMsg, tea.MouseReleaseMsg:
		if !s.selecting {
			return nil, false
		}
		s.selHead = s.textAt(x, y)
		s.selected = s.selected || s.selHead != s.selAnchor
		if _, released := msg.(tea.MouseReleaseMsg); released {
			s.selecting = false
			if text := s.selectedText(); text != "" {
				return copyText(text), true
			}
		}
		return nil, true
	}
	return nil, false
}

// copyText puts text on the system clipboard, then asks the real terminal
// to as well, for when there is no system clipboard to write.
func copyText(text string) tea.Cmd {
	return tea.Sequence(func() tea.Msg {
		_ = clipboard.WriteText(text)
		return nil
	}, tea.SetClipboard(text))
}

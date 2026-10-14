package sessionhost

import (
	"slices"
	"strconv"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
)

func (h *Host) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	if h.confirm != nil {
		switch msg.String() {
		case "left", "right", "tab", "shift+tab", "h", "l":
			h.confirm.keep = !h.confirm.keep
		case "enter", "space":
			h.answerClose(!h.confirm.keep)
		case "y", "Y":
			h.answerClose(true)
		case "esc", "n", "N":
			h.answerClose(false)
		}
		return nil
	}
	if h.picker != nil {
		h.pickerKey(msg)
		return nil
	}
	if h.serverKey(msg.String()) {
		return nil
	}
	switch key := msg.String(); key {
	case "alt+n":
		h.openPicker()
		return nil
	case "alt+t":
		h.openPickerFor(terminalSession)
		return nil
	case "alt+s":
		h.toggleList()
		return nil
	case "alt+w":
		h.askClose(h.active)
		return nil
	case "alt+shift+up":
		h.moveActive(h.active - 1)
		return nil
	case "alt+shift+down":
		h.moveActive(h.active + 1)
		return nil
	case "alt+1", "alt+2", "alt+3", "alt+4", "alt+5", "alt+6", "alt+7", "alt+8", "alt+9":
		n, _ := strconv.Atoi(key[len("alt+"):])
		h.show(n - 1)
		return nil
	}
	if s := h.current(); s != nil {
		if s.terminalKey(msg) {
			return nil
		}
		s.clearSelection()
		s.scrollOffset = 0
		if text, key := sessionInput(msg); text != "" {
			s.emu.SendText(text)
		} else {
			s.emu.SendKey(key)
		}
	}
	return nil
}

func (h *Host) pickerKey(msg tea.KeyPressMsg) {
	p := h.picker
	switch msg.String() {
	case "esc":
		h.picker = nil
	case "enter":
		h.startPicked()
	case "up", "shift+tab", "ctrl+p":
		p.move(-1)
	case "down", "ctrl+n":
		p.move(1)
	case "tab":
		p.complete()
	case "left":
		p.cursor = max(p.cursor-1, 0)
	case "right":
		p.cursor = min(p.cursor+1, len(p.input))
	case "home", "ctrl+a":
		p.cursor = 0
	case "end", "ctrl+e":
		p.cursor = len(p.input)
	case "backspace":
		p.backspace()
	case "delete", "ctrl+d":
		p.deleteForward()
	case "ctrl+w", "alt+backspace":
		p.deleteWord()
	case "ctrl+u":
		p.setInput("")
	default:
		if text, _ := sessionInput(msg); text != "" && !strings.HasPrefix(text, "\x1b") {
			p.insert(text)
		}
	}
}

// sessionInput returns what to send a session for a key press: the text
// the key types, or else the key for the emulator to encode. The emulator
// encodes keys the classic way and types a character only when no modifier
// is held, so Shift+A, Shift+1 and keys of other layouts go as their text;
// lock keys don't count as modifiers; and Shift+Enter, which has no classic
// form, goes as Ctrl+J, Crush's other new-line key.
func sessionInput(msg tea.KeyPressMsg) (string, uv.KeyPressEvent) {
	key := uv.KeyPressEvent(msg)
	key.Mod &^= uv.ModCapsLock | uv.ModNumLock | uv.ModScrollLock
	alt := ""
	if key.Mod&uv.ModAlt != 0 {
		alt = "\x1b"
	}
	rest := key.Mod &^ uv.ModAlt
	switch {
	case key.Code == uv.KeyEnter && rest == uv.ModShift:
		return "", uv.KeyPressEvent{Code: 'j', Mod: uv.ModCtrl}
	case key.Text != "" && rest&^uv.ModShift == 0:
		return alt + key.Text, key
	case rest == uv.ModShift && unicode.IsPrint(key.ShiftedCode):
		return alt + string(key.ShiftedCode), key
	}
	return "", key
}

func (h *Host) handleMouse(msg tea.MouseMsg) tea.Cmd {
	m := msg.Mouse()
	side := h.sideWidth()
	if h.confirm != nil {
		if click, ok := msg.(tea.MouseClickMsg); ok && click.Button == tea.MouseLeft {
			h.clickConfirm(m.X, m.Y)
		}
		return nil
	}
	if h.picker != nil {
		if click, ok := msg.(tea.MouseClickMsg); ok && click.Button == tea.MouseLeft {
			h.clickPicker(m.X, m.Y)
		}
		return nil
	}
	if h.dragging {
		switch msg.(type) {
		case tea.MouseMotionMsg:
			if m.Button == tea.MouseLeft {
				h.dragTo(m.Y)
				return nil
			}
			h.dragging = false
		case tea.MouseReleaseMsg:
			h.dragging = false
			return nil
		default:
			h.dragging = false
		}
	}
	if _, ok := msg.(tea.MouseClickMsg); ok && (m.X >= side || !h.rowAt(m.X, m.Y).onServer) {
		h.unpick()
	}
	if m.X >= side || h.capture {
		return h.forwardMouse(msg, side)
	}
	switch msg := msg.(type) {
	case tea.MouseMotionMsg:
		h.hover = h.rowAt(m.X, m.Y).session
	case tea.MouseClickMsg:
		if msg.Button == tea.MouseLeft {
			h.clickSide(m.X, m.Y)
		}
	case tea.MouseWheelMsg:
		// The list has no scrolling of its own; the shown session is kept
		// in view instead.
	}
	return nil
}

// forwardMouse passes a mouse event over the session on to it, moved into
// its own coordinates.
func (h *Host) forwardMouse(msg tea.MouseMsg, side int) tea.Cmd {
	s := h.current()
	if s == nil {
		return nil
	}
	m := uv.Mouse(msg.Mouse())
	m.X = max(m.X-side, 0)
	h.hover = -1
	if s.terminalMouse(msg) {
		return nil
	}
	if cmd, ok := s.terminalSelect(msg, m.X, m.Y); ok {
		_, released := msg.(tea.MouseReleaseMsg)
		h.capture = !released
		return cmd
	}
	switch msg.(type) {
	case tea.MouseClickMsg:
		h.capture = true
		s.emu.SendMouse(uv.MouseClickEvent(m))
	case tea.MouseReleaseMsg:
		h.capture = false
		s.emu.SendMouse(uv.MouseReleaseEvent(m))
	case tea.MouseMotionMsg:
		if s.wantsMotion(m.Button != uv.MouseNone) {
			s.emu.SendMouse(uv.MouseMotionEvent(m))
		}
	case tea.MouseWheelMsg:
		s.emu.SendMouse(uv.MouseWheelEvent(m))
	}
	return nil
}

func (h *Host) clickSide(x, y int) {
	r := h.rowAt(x, y)
	switch {
	case r.toggle:
		h.toggleList()
	case r.newSession:
		h.openPicker()
	case r.newTerminal:
		h.openPickerFor(terminalSession)
	case r.stopServer:
		if row, ok := h.picked(); ok {
			h.stopServer(row.server)
		}
	case r.onServer && (h.isPicked(r.server) || r.server.ports > 0):
		h.pick(r.server)
		h.useRow(r.server)
	case r.onServer:
		h.pick(r.server)
	case r.session >= 0 && r.close:
		h.askClose(r.session)
	case r.session >= 0:
		h.show(r.session)
		h.dragging = true
	}
}

// dragTo moves the shown tab to the list row at y, inside its group.
func (h *Host) dragTo(y int) {
	block := stripBlockHeight
	if h.listOpen() {
		block = listBlockHeight
	}
	first, count := h.visibleRange(block)
	to := first - 1
	switch {
	case h.listOpen():
		to = first + h.listBlockAt(y, first, count)
	case y >= h.firstRow():
		to = first + (y-h.firstRow())/block
	}
	h.moveActive(to)
}

// moveActive moves the shown tab to place to, kept inside its group:
// chats come first, then terminals.
func (h *Host) moveActive(to int) {
	s := h.current()
	if s == nil {
		return
	}
	chats := 0
	for _, other := range h.sessions {
		if other.kind == chatSession {
			chats++
		}
	}
	first, last := 0, chats-1
	if s.kind == terminalSession {
		first, last = chats, len(h.sessions)-1
	}
	to = min(max(to, first), last)
	if to == h.active {
		return
	}
	h.sessions = slices.Insert(slices.Delete(h.sessions, h.active, h.active+1), to, s)
	h.active = to
	h.hover = -1
}

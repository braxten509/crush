package sessionhost

import (
	"image/color"
	"log/slog"
	"slices"
	"strconv"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/ui/styles"
	uv "github.com/charmbracelet/ultraviolet"
)

const (
	// listWidth and stripWidth include the column with the dividing line.
	listWidth  = 30
	stripWidth = 4
	// minWidthForList: below this the open list would squeeze the session
	// too much, so the strip is shown instead (the saved choice is kept).
	minWidthForList = 80
	// colorWait is how long the host waits to learn the terminal's colors
	// before starting the first session anyway.
	colorWait = 300 * time.Millisecond
)

// Options says how the host starts sessions.
type Options struct {
	// Exe is the Crush executable.
	Exe string
	// FirstArgs are the arguments of the first session: the ones the host
	// was started with.
	FirstArgs []string
	// NewArgs returns the arguments of a session started with
	// "+ New session" in dir.
	NewArgs func(dir string) []string
	// Dir is the folder the first session starts in.
	Dir string
	// Env is the environment sessions start with.
	Env []string
}

type colorWaitMsg struct{}

// Host is the Bubble Tea model of the session list and the shown session.
type Host struct {
	opts      Options
	send      func(tea.Msg)
	prefsPath string
	prefs     prefs

	sessions []*session
	active   int
	nextID   int

	width, height int
	fg, bg        color.Color
	colorsKnown   bool
	waited        bool
	started       bool

	theme  string
	styles styles.Styles

	hover   int // hovered list row; -1 for none
	confirm *confirmClose
	// capture: a mouse button went down over the session, so its motion
	// and release go there even when the pointer leaves it.
	capture bool
	notice  string // why a new session couldn't start
	// blurred: the real terminal window lost focus.
	blurred bool
	// startErr is why the first session couldn't start.
	startErr error
}

type confirmClose struct {
	id   int
	keep bool // the Keep button is selected rather than Close
}

// New returns a host. send delivers messages to the running program; set
// it with SetSend before the program starts.
func New(opts Options) *Host {
	h := &Host{opts: opts, hover: -1, styles: styles.ThemeFromConfig("")}
	if path, err := prefsPath(); err == nil {
		h.prefsPath = path
		h.prefs = loadPrefs(path)
	}
	return h
}

// SetSend sets how sessions deliver messages to the program.
func (h *Host) SetSend(send func(tea.Msg)) { h.send = send }

func (h *Host) Init() tea.Cmd {
	return tea.Batch(
		tea.RequestBackgroundColor,
		tea.RequestForegroundColor,
		tea.Tick(colorWait, func(time.Time) tea.Msg { return colorWaitMsg{} }),
	)
}

func (h *Host) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		h.width, h.height = msg.Width, msg.Height
		w, ht := h.sessionSize()
		for _, s := range h.sessions {
			s.resize(w, ht)
		}
		return h, h.maybeStart()
	case tea.BackgroundColorMsg:
		h.bg, h.colorsKnown = msg.Color, true
		return h, h.maybeStart()
	case tea.ForegroundColorMsg:
		h.fg = msg.Color
	case colorWaitMsg:
		h.waited = true
		return h, h.maybeStart()
	case outputMsg:
		if s := h.byID(msg.id); s != nil {
			s.dirty.Store(false)
		}
	case statusMsg:
		h.statusChanged(msg.id)
	case exitedMsg:
		return h, h.removeSession(msg.id)
	case tea.KeyPressMsg:
		return h, h.handleKey(msg)
	case tea.PasteMsg:
		if s := h.current(); s != nil && h.confirm == nil {
			s.emu.Paste(msg.Content)
		}
	case tea.MouseMsg:
		h.handleMouse(msg)
	case clipboardMsg:
		return h, tea.SetClipboard(msg.text)
	case tea.FocusMsg:
		h.blurred = false
		if s := h.current(); s != nil {
			s.setFocus(true)
		}
	case tea.BlurMsg:
		h.blurred = true
		if s := h.current(); s != nil {
			s.setFocus(false)
		}
	}
	return h, nil
}

// maybeStart starts the first session once the window size is known and
// the terminal's colors are known or took too long, so the session's color
// queries get the real answers.
func (h *Host) maybeStart() tea.Cmd {
	if h.started || h.width == 0 || !(h.colorsKnown || h.waited) {
		return nil
	}
	h.started = true
	if err := h.startSession(h.opts.Dir, h.opts.FirstArgs); err != nil {
		h.startErr = err
		return tea.Quit
	}
	return nil
}

func (h *Host) startSession(dir string, args []string) error {
	w, ht := h.sessionSize()
	h.nextID++
	l := launch{exe: h.opts.Exe, args: args, dir: dir, env: h.opts.Env}
	s, err := startSession(h.nextID, l, w, ht, h.fg, h.bg, h.send)
	if err != nil {
		return err
	}
	h.sessions = append(h.sessions, s)
	h.show(len(h.sessions) - 1)
	return nil
}

// newSession starts a session in the folder of the one being viewed.
func (h *Host) newSession() {
	dir := h.opts.Dir
	if s := h.current(); s != nil {
		if d := s.snapshot().Dir; d != "" {
			dir = d
		}
	}
	h.notice = ""
	if err := h.startSession(dir, h.opts.NewArgs(dir)); err != nil {
		slog.Error("Could not start a Crush session", "error", err)
		h.notice = "Couldn't start a session."
	}
}

func (h *Host) show(i int) {
	if i < 0 || i >= len(h.sessions) {
		return
	}
	if s := h.current(); s != nil && s != h.sessions[i] {
		s.visible.Store(false)
		s.setFocus(false)
	}
	h.active = i
	s := h.sessions[i]
	s.visible.Store(true)
	s.setFocus(!h.blurred)
	s.dirty.Store(false)
	s.unread = false
	h.applyTheme(s.snapshot().Theme)
}

func (h *Host) current() *session {
	if h.active < 0 || h.active >= len(h.sessions) {
		return nil
	}
	return h.sessions[h.active]
}

func (h *Host) byID(id int) *session {
	for _, s := range h.sessions {
		if s.id == id {
			return s
		}
	}
	return nil
}

func (h *Host) statusChanged(id int) {
	s := h.byID(id)
	if s == nil {
		return
	}
	st := s.snapshot()
	working := st.State == StateWorking
	// Work that ends while another session is shown waits as "finished"
	// until the user looks.
	if s.working && !working && st.State == StateReady && s != h.current() {
		s.unread = true
	}
	if working {
		s.unread = false
	}
	s.working = working
	if s == h.current() {
		h.applyTheme(st.Theme)
	}
}

func (h *Host) applyTheme(name string) {
	if name == h.theme {
		return
	}
	h.theme = name
	h.styles = styles.ThemeFromConfig(name)
}

func (h *Host) removeSession(id int) tea.Cmd {
	i := slices.IndexFunc(h.sessions, func(s *session) bool { return s.id == id })
	if i < 0 {
		return nil
	}
	h.sessions[i].visible.Store(false)
	h.sessions = slices.Delete(h.sessions, i, i+1)
	if h.confirm != nil && h.confirm.id == id {
		h.confirm = nil
	}
	h.hover = -1
	if len(h.sessions) == 0 {
		return tea.Quit
	}
	switch {
	case i < h.active:
		h.active--
	case i == h.active:
		h.active = min(i, len(h.sessions)-1)
	}
	h.show(h.active)
	return nil
}

// listOpen reports whether the full list is shown rather than the strip.
func (h *Host) listOpen() bool {
	return !h.prefs.ListClosed && h.width >= minWidthForList
}

func (h *Host) sideWidth() int {
	if h.listOpen() {
		return listWidth
	}
	return stripWidth
}

func (h *Host) sessionSize() (int, int) {
	return max(h.width-h.sideWidth(), 1), max(h.height, 1)
}

func (h *Host) toggleList() {
	h.prefs.ListClosed = !h.prefs.ListClosed
	h.hover = -1
	if h.prefsPath != "" {
		if err := savePrefs(h.prefsPath, h.prefs); err != nil {
			slog.Warn("Could not save the session list setting", "error", err)
		}
	}
	w, ht := h.sessionSize()
	for _, s := range h.sessions {
		s.resize(w, ht)
	}
}

func (h *Host) askClose(i int) {
	if i < 0 || i >= len(h.sessions) {
		return
	}
	h.confirm = &confirmClose{id: h.sessions[i].id}
}

func (h *Host) answerClose(close bool) {
	c := h.confirm
	h.confirm = nil
	if !close {
		return
	}
	if s := h.byID(c.id); s != nil {
		s.stop()
	}
}

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
	switch key := msg.String(); key {
	case "alt+s":
		h.toggleList()
		return nil
	case "alt+w":
		h.askClose(h.active)
		return nil
	case "alt+1", "alt+2", "alt+3", "alt+4", "alt+5", "alt+6", "alt+7", "alt+8", "alt+9":
		n, _ := strconv.Atoi(key[len("alt+"):])
		h.show(n - 1)
		return nil
	}
	if s := h.current(); s != nil {
		if text, key := sessionInput(msg); text != "" {
			s.emu.SendText(text)
		} else {
			s.emu.SendKey(key)
		}
	}
	return nil
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

func (h *Host) handleMouse(msg tea.MouseMsg) {
	m := msg.Mouse()
	side := h.sideWidth()
	if h.confirm != nil {
		if click, ok := msg.(tea.MouseClickMsg); ok && click.Button == tea.MouseLeft {
			h.clickConfirm(m.X, m.Y)
		}
		return
	}
	if m.X >= side || h.capture {
		h.forwardMouse(msg, side)
		return
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
}

// forwardMouse passes a mouse event over the session on to it, moved into
// its own coordinates.
func (h *Host) forwardMouse(msg tea.MouseMsg, side int) {
	s := h.current()
	if s == nil {
		return
	}
	m := uv.Mouse(msg.Mouse())
	m.X = max(m.X-side, 0)
	h.hover = -1
	switch msg.(type) {
	case tea.MouseClickMsg:
		h.capture = true
		s.emu.SendMouse(uv.MouseClickEvent(m))
	case tea.MouseReleaseMsg:
		h.capture = false
		s.emu.SendMouse(uv.MouseReleaseEvent(m))
	case tea.MouseMotionMsg:
		s.emu.SendMouse(uv.MouseMotionEvent(m))
	case tea.MouseWheelMsg:
		s.emu.SendMouse(uv.MouseWheelEvent(m))
	}
}

func (h *Host) clickSide(x, y int) {
	r := h.rowAt(x, y)
	switch {
	case r.toggle:
		h.toggleList()
	case r.newSession:
		h.newSession()
	case r.session >= 0 && r.close:
		h.askClose(r.session)
	case r.session >= 0:
		h.show(r.session)
	}
}

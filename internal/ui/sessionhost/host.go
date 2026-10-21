package sessionhost

import (
	"image/color"
	"log/slog"
	"slices"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/openchats"
	"github.com/charmbracelet/crush/internal/ui/styles"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
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
	// "+ Session" in dir.
	NewArgs func(dir string) []string
	// Dir is the folder the first session starts in.
	Dir string
	// Env is the environment sessions start with.
	Env []string
	// Shell and ShellEnv start a user's normal interactive terminal.
	Shell    string
	ShellEnv []string
	// Recent returns folders Crush was used in, the latest first, for the
	// new-session box. It may be nil.
	Recent func() []string
}

type colorWaitMsg struct{}

// Host is the Bubble Tea model of the session list and the shown session.
type Host struct {
	opts      Options
	send      func(tea.Msg)
	prefsPath string
	prefs     prefs

	sessions []*session
	closing  map[int]*session
	active   int
	nextID   int

	width, height int
	fg, bg        color.Color
	colorsKnown   bool
	waited        bool
	started       bool

	theme  string
	styles styles.Styles
	// themeRevision is the shown theme file's revision, as the last
	// session reported it.
	themeRevision string
	// useTerminalBackground follows the last shown chat's background setting.
	useTerminalBackground bool
	appearanceSource      int // last shown chat, including later status updates

	activityFrame   int
	activityTicking bool

	// kitty: the real terminal shows Kitty graphics. pixelWidth and
	// pixelHeight are its size in pixels, when it said.
	kitty                   bool
	pixelWidth, pixelHeight int
	// pictures tracks the Kitty graphics placed on the real terminal.
	pictures screenPictures

	hover int // hovered list row; -1 for none
	// pickedServer is the port of the server picked in the list, and
	// pickedFold the folded program picked instead; the picked row takes
	// the keys until let go. 0 and "" for none.
	pickedServer int
	pickedFold   string
	// openFolds are the folded programs opened to list their servers.
	openFolds map[string]bool
	confirm   *confirmClose
	picker    *picker // the open new-session box, if any
	// capture: a mouse button went down over the session, so its motion
	// and release go there even when the pointer leaves it.
	capture bool
	// dragging: the shown tab was pressed in the list and moves with the
	// mouse until the button is let go.
	dragging bool
	notice   string // why a new session couldn't start
	// blurred: the real terminal window lost focus.
	blurred bool
	// startErr is why the first session couldn't start.
	startErr error
	// chats is the open-chats list shared with Crush Desktop (shared.go);
	// nil when the host doesn't share it. leaving: the host is quitting.
	chats   *openchats.List
	leaving bool
}

type confirmClose struct {
	id   int
	keep bool // the Keep button is selected rather than Close
}

// New returns a host. send delivers messages to the running program; set
// it with SetSend before the program starts.
func New(opts Options) *Host {
	// Start in the saved theme so the window never shows the default
	// before the first chat reports its own.
	theme := config.SavedTheme()
	h := &Host{opts: opts, hover: -1, theme: theme, styles: styles.ThemeFromConfig(theme)}
	if path, err := prefsPath(); err == nil {
		h.prefsPath = path
		h.prefs = loadPrefs(path)
	}
	return h
}

// SetSend sets how sessions deliver messages to the program.
func (h *Host) SetSend(send func(tea.Msg)) { h.send = send }

// pictureQuery asks the terminal whether it shows Kitty graphics, and its
// size in pixels. Asked before the colors, its answers come first, so they
// are known when the first session starts.
var pictureQuery = ansi.KittyGraphics([]byte("AAAA"), "i=31", "s=1", "v=1", "a=q", "t=d", "f=24") +
	ansi.WindowOp(14)

func (h *Host) Init() tea.Cmd {
	return tea.Sequence(
		tea.Raw(pictureQuery),
		tea.Batch(
			tea.RequestBackgroundColor,
			tea.RequestForegroundColor,
			tea.Tick(colorWait, func(time.Time) tea.Msg { return colorWaitMsg{} }),
		),
		h.pollOpenChats(),
	)
}

func (h *Host) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	cmd := h.update(msg)
	cmd = tea.Batch(cmd, h.tickActivity())
	h.syncPictures()
	if raw := h.pictures.takeRaw(); raw != "" {
		cmd = tea.Batch(cmd, tea.Raw(raw))
	}
	return h, cmd
}

func (h *Host) update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		h.width, h.height = msg.Width, msg.Height
		w, ht := h.sessionSize()
		for _, s := range h.sessions {
			s.resize(w, ht)
			h.shareCellSize(s)
		}
		return h.maybeStart()
	case tea.BackgroundColorMsg:
		h.bg, h.colorsKnown = msg.Color, true
		h.refreshTerminalColors()
		return h.maybeStart()
	case tea.ForegroundColorMsg:
		h.fg = msg.Color
	case colorWaitMsg:
		h.waited = true
		return h.maybeStart()
	case outputMsg:
		if s := h.byID(msg.id); s != nil {
			s.dirty.Store(false)
			s.refreshScrollback()
		}
	case statusMsg:
		h.statusChanged(msg.id)
	case activityTickMsg:
		h.activityTicking = false
		h.activityFrame = 0
		at := msg.Time
		if at.IsZero() {
			at = time.Now()
		}
		return h.tickActivityAt(at)
	case exitedMsg:
		h.unlistExited(h.byID(msg.id))
		return h.removeSession(msg.id)
	case openChatsTickMsg:
		h.syncOpenChats()
		return h.pollOpenChats()
	case tea.KeyPressMsg:
		return h.handleKey(msg)
	case tea.PasteMsg:
		if h.picker != nil {
			h.picker.insert(msg.Content)
		} else if s := h.current(); s != nil && h.confirm == nil {
			s.scrollOffset = 0
			s.emu.Paste(msg.Content)
		}
	case tea.MouseMsg:
		return h.handleMouse(msg)
	case clipboardMsg:
		return tea.SetClipboard(msg.text)
	case picturesMsg:
		if s := h.byID(msg.id); s != nil {
			h.takePictures(s, msg.cmds)
		}
	case uv.KittyGraphicsEvent:
		h.kitty = true
		for _, s := range h.sessions {
			s.kitty.Store(true)
		}
	case uv.PixelSizeEvent:
		h.pixelWidth, h.pixelHeight = msg.Width, msg.Height
		for _, s := range h.sessions {
			h.shareCellSize(s)
		}
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
	return nil
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
	s, err := startSession(h.nextID, l, w, ht, h.fg, h.bg, h.send, func(s *session) {
		s.kitty.Store(h.kitty)
		h.shareCellSize(s)
	})
	if err != nil {
		return err
	}
	h.addSession(s)
	return nil
}

// addSession lists s and shows it. Chats go after the other chats, above
// the terminals.
func (h *Host) addSession(s *session) {
	i := len(h.sessions)
	if s.kind == chatSession {
		for i > 0 && h.sessions[i-1].kind == terminalSession {
			i--
		}
	}
	h.sessions = slices.Insert(h.sessions, i, s)
	h.show(i)
}

// openPicker opens the new-session box at the folder of the session being
// viewed.
func (h *Host) openPicker() {
	h.openPickerFor(chatSession)
}

func (h *Host) openPickerFor(kind sessionKind) {
	if h.confirm != nil {
		return
	}
	dir := h.opts.Dir
	if s := h.current(); s != nil {
		if d := s.workingDir(); d != "" {
			dir = d
		}
	}
	var recent []string
	if h.opts.Recent != nil {
		recent = h.opts.Recent()
	}
	h.picker = newPicker(dir, recent)
	h.picker.kind = kind
	h.hover = -1
}

// startPicked starts a session in the folder chosen in the new-session box,
// or says in the box why it can't.
func (h *Host) startPicked() {
	dir, ok := h.picker.choice()
	if !ok {
		return
	}
	kind := h.picker.kind
	h.picker = nil
	if kind == terminalSession {
		h.newTerminal(dir)
	} else {
		h.newSession(dir)
	}
}

// newSession starts a session in dir.
func (h *Host) newSession(dir string) {
	h.notice = ""
	if err := h.startSession(dir, h.opts.NewArgs(dir)); err != nil {
		slog.Error("Could not start a Crush session", "error", err)
		h.notice = "Couldn't start a session."
	}
}

// shareCellSize tells s the real terminal's character size in pixels.
func (h *Host) shareCellSize(s *session) {
	if h.pixelWidth <= 0 || h.pixelHeight <= 0 || h.width <= 0 || h.height <= 0 {
		return
	}
	s.cellWidth.Store(int32(h.pixelWidth / h.width))
	s.cellHeight.Store(int32(h.pixelHeight / h.height))
}

func (h *Host) show(i int) {
	if i < 0 || i >= len(h.sessions) || !h.wake(i) {
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
	s.refreshScrollback()
	if s.kind == chatSession {
		h.appearanceSource = s.id
		// A chat that is still starting keeps the current theme until it
		// reports its own, rather than showing the default for a moment.
		if st, ok := s.reportedStatus(); ok {
			h.applyAppearance(st)
		}
	}
}

func (h *Host) current() *session {
	// A waiting row has no screen until it is shown (shared.go).
	if h.active < 0 || h.active >= len(h.sessions) || h.sessions[h.active].waiting {
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
	h.listChat(s, st)
	working := st.State == StateWorking || st.State == StateBackground
	// Work that ends while another session is shown waits as "finished"
	// until the user looks.
	if s.working && !working && st.State == StateReady && s != h.current() {
		s.unread = true
	}
	if working {
		s.unread = false
	}
	s.working = working
	current := h.current()
	if s.kind == chatSession && (s == current || current != nil && current.kind == terminalSession && s.id == h.appearanceSource) {
		h.applyAppearance(st)
	}
}

func (h *Host) applyTheme(name, revision string) {
	if name == h.theme && revision == h.themeRevision {
		return
	}
	h.theme, h.themeRevision = name, revision
	h.styles = styles.ThemeFromConfig(name)
	h.refreshTerminalColors()
}

func (h *Host) removeSession(id int) tea.Cmd {
	i := slices.IndexFunc(h.sessions, func(s *session) bool { return s.id == id })
	if i < 0 {
		delete(h.closing, id)
		if len(h.sessions) == 0 && len(h.closing) == 0 {
			return tea.Quit
		}
		return nil
	}
	h.sessions[i].visible.Store(false)
	h.sessions = slices.Delete(h.sessions, i, i+1)
	if h.confirm != nil && h.confirm.id == id {
		h.confirm = nil
	}
	h.hover = -1
	if len(h.sessions) == 0 {
		if len(h.closing) == 0 {
			return tea.Quit
		}
		return nil
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
		s.closedHere = true
		if s.waiting {
			h.unlistExited(s)
			h.dropRow(s.id)
			return
		}
		s.stop()
		if s.kind == terminalSession {
			if h.closing == nil {
				h.closing = make(map[int]*session)
			}
			h.closing[s.id] = s
			h.removeSession(s.id)
		}
	}
}

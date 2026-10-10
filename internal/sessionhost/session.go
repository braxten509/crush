package sessionhost

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
	"github.com/creack/pty"
)

// launch says how to start one session's Crush.
type launch struct {
	exe  string
	args []string
	dir  string
	env  []string
}

// Messages a session sends the host. None is sent while the emulator's
// lock is held: the host draws the emulator, so a send that waits for the
// host from inside a callback would never return.
type (
	outputMsg    struct{ id int } // the shown session drew something
	statusMsg    struct{ id int } // the session reported a new Status
	exitedMsg    struct{ id int } // the session's Crush ended
	clipboardMsg struct{ text string }
	// picturesMsg holds a session's Kitty graphics commands for the real
	// terminal.
	picturesMsg struct {
		id   int
		cmds []pictureCmd
	}
)

// pictureCmd is one Kitty graphics command from a session's Crush.
type pictureCmd struct {
	seq string // the whole command
	// place: part of putting picture id at the session's cell at. Crush
	// moved the cursor there first; that move stayed in the emulator, so
	// the host puts the picture there itself, and takes it down and back
	// as the session is hidden and shown. first starts a new picture.
	place, first bool
	at           image.Point
	// remove: takes down picture id, or every picture when id is "".
	remove bool
	id     string
}

// stopGrace is how long a closed session gets to save and exit before it
// is killed.
const stopGrace = 5 * time.Second

// outputDrain is how long an ended session's last output may take.
const outputDrain = 2 * time.Second

// session is one Crush process in its own hidden terminal.
type session struct {
	id   int
	kind sessionKind
	emu  *vt.SafeEmulator
	tty  *os.File
	cmd  *exec.Cmd

	// visible is set while the host shows this session, so only its output
	// asks for a new frame. dirty holds back repeat requests until the host
	// has drawn.
	visible atomic.Bool
	dirty   atomic.Bool
	// changed is set by the status callback and sent by the output loop.
	changed atomic.Bool
	exited  atomic.Bool
	// focusEvents: Crush asked to hear when its window gains or loses
	// focus. It notifies about finished work only while unfocused, so a
	// hidden session must hear that it lost focus.
	focusEvents atomic.Bool
	// anyMotion and buttonMotion: Crush asked for every mouse move, or for
	// moves while a button is held. The emulator would pass every move on
	// either way, so the host keeps to what was asked.
	anyMotion    atomic.Bool
	buttonMotion atomic.Bool
	copied       atomic.Bool

	// kitty: the real terminal shows Kitty graphics. Crush then sends its
	// pictures once and marks their cells with placeholder characters,
	// which the emulator keeps as text; the host passes the pictures on.
	kitty atomic.Bool
	// cellWidth and cellHeight are the real terminal's character size in
	// pixels, when known, so pictures keep their shape.
	cellWidth, cellHeight atomic.Int32
	pictured              atomic.Bool
	// placing is the picture being put on screen, while its parts come in.
	// Only the emulator's callbacks use it.
	placing *pictureCmd

	mu      sync.Mutex
	status  Status
	cursor  bool
	shape   vt.CursorStyle
	blink   bool
	stopped bool
	clip    string // text Crush last copied, for the real clipboard
	// pictures are Kitty graphics commands not yet passed on.
	pictures []pictureCmd

	// Kept by the host's update loop only.
	unread  bool // finished work the user hasn't looked at yet
	working bool // last reported state was working or waiting for helpers
	// Terminal history position; owned by the host update loop.
	scrollOffset    int
	scrollbackLen   int
	scrollbackTotal uint64
	// placed are the pictures this session put on screen, by id, to take
	// down and put back.
	placed map[string]*placedPicture
}

// newSession returns a session's emulator, set up to follow what Crush
// reports through it; startSession then starts Crush.
func newSession(id, width, height int, fg, bg color.Color) *session {
	emu := vt.NewSafeEmulator(width, height)
	emu.SetDefaultForegroundColor(fg)
	emu.SetDefaultBackgroundColor(bg)
	s := &session{id: id, emu: emu, cursor: true, placed: map[string]*placedPicture{}}
	emu.SetCallbacks(vt.Callbacks{
		CursorVisibility: func(visible bool) {
			s.mu.Lock()
			s.cursor = visible
			s.mu.Unlock()
		},
		CursorStyle: func(style vt.CursorStyle, blink bool) {
			s.mu.Lock()
			s.shape, s.blink = style, blink
			s.mu.Unlock()
		},
		EnableMode:       func(mode ansi.Mode) { s.setMode(mode, true) },
		DisableMode:      func(mode ansi.Mode) { s.setMode(mode, false) },
		WorkingDirectory: s.setWorkingDir,
	})
	// Copying in Crush writes OSC 52, which the emulator would keep to
	// itself; pass it on to the real terminal.
	emu.RegisterOscHandler(52, func(data []byte) bool {
		parts := bytes.Split(data, []byte{';'})
		if len(parts) != 3 || string(parts[2]) == "?" {
			return true
		}
		text, err := base64.StdEncoding.DecodeString(string(parts[2]))
		if err == nil {
			s.mu.Lock()
			s.clip = string(text)
			s.mu.Unlock()
			s.copied.Store(true)
		}
		return true
	})
	emu.RegisterApcHandler(s.handleGraphics)
	emu.RegisterCsiHandler(ansi.Command(0, 0, 't'), s.handleWindowOp)
	emu.RegisterOscHandler(statusOSC, func(data []byte) bool {
		if s.kind == terminalSession {
			return true
		}
		if st, ok := parseStatus(data); ok {
			s.mu.Lock()
			s.status = st
			s.mu.Unlock()
			s.changed.Store(true)
		}
		return true
	})
	return s
}

// handleGraphics answers Crush's Kitty graphics question for the real
// terminal and keeps its pictures to pass on. It runs with the emulator
// locked, so replies go out from another goroutine.
func (s *session) handleGraphics(data []byte) bool {
	if len(data) == 0 || data[0] != 'G' {
		return false
	}
	if !s.kitty.Load() {
		// No answer: Crush draws pictures with characters instead.
		return true
	}
	control, _, _ := strings.Cut(string(data[1:]), ";")
	keys := map[string]string{}
	for _, kv := range strings.Split(control, ",") {
		k, v, _ := strings.Cut(kv, "=")
		keys[k] = v
	}
	if keys["a"] == "q" {
		reply := "\x1b_Gi=" + keys["i"] + ";OK\x1b\\"
		go func() { _, _ = io.WriteString(s.emu.InputPipe(), reply) }()
		return true
	}
	cmd := pictureCmd{seq: "\x1b_" + string(data) + "\x1b\\"}
	action, hasAction := keys["a"]
	switch {
	case !hasAction && s.placing != nil:
		// The next part of the picture being placed.
		cmd.place, cmd.id, cmd.at = true, s.placing.id, s.placing.at
	case (action == "T" || action == "p") && keys["U"] != "1":
		// Placeholder pictures (U=1) take their place from text cells;
		// the others go where the cursor is. The emulator is locked, so
		// read the cursor without locking it again.
		pos := s.emu.Emulator.CursorPosition()
		cmd.place, cmd.first, cmd.id, cmd.at = true, true, keys["i"], image.Pt(pos.X, pos.Y)
		s.placing = &cmd
	case action == "d" && (keys["d"] == "i" || keys["d"] == "I"):
		cmd.remove, cmd.id = true, keys["i"]
	case action == "d" && (keys["d"] == "" || keys["d"] == "a" || keys["d"] == "A"):
		cmd.remove = true
	}
	if keys["m"] != "1" {
		s.placing = nil
	}
	s.mu.Lock()
	s.pictures = append(s.pictures, cmd)
	s.mu.Unlock()
	s.pictured.Store(true)
	return true
}

// handleWindowOp answers Crush's question for the window's size in pixels,
// from the real terminal's character size.
func (s *session) handleWindowOp(params ansi.Params) bool {
	if op, _, _ := params.Param(0, 0); op != 14 {
		return false
	}
	w, h := int(s.cellWidth.Load()), int(s.cellHeight.Load())
	if w == 0 || h == 0 {
		return false
	}
	// The emulator is locked; read its size without locking it again.
	cols, rows := s.emu.Emulator.Width(), s.emu.Emulator.Height()
	reply := fmt.Sprintf("\x1b[4;%d;%dt", rows*h, cols*w)
	go func() { _, _ = io.WriteString(s.emu.InputPipe(), reply) }()
	return true
}

// setMode follows the terminal modes the host acts on.
func (s *session) setMode(mode ansi.Mode, on bool) {
	switch mode {
	case ansi.ModeFocusEvent:
		s.focusEvents.Store(on)
	case ansi.ModeMouseAnyEvent:
		s.anyMotion.Store(on)
	case ansi.ModeMouseButtonEvent:
		s.buttonMotion.Store(on)
	}
}

// wantsMotion reports whether Crush asked to hear a mouse move, held is
// whether a button is down during it.
func (s *session) wantsMotion(held bool) bool {
	return s.anyMotion.Load() || (held && s.buttonMotion.Load())
}

// startSession starts a session's Crush; setup readies the session first.
func startSession(id int, l launch, width, height int, fg, bg color.Color, send func(tea.Msg), setup func(*session)) (*session, error) {
	s := newSession(id, width, height, fg, bg)
	setup(s)
	emu := s.emu
	cmd := exec.Command(l.exe, l.args...)
	cmd.Dir = l.dir
	cmd.Env = l.env
	tty, err := pty.StartWithSize(cmd, winsize(width, height))
	if err != nil {
		_ = emu.Close()
		return nil, err
	}
	s.tty, s.cmd = tty, cmd

	outputDone := make(chan struct{})
	go func() {
		s.copyOutput(send)
		close(outputDone)
	}()
	go s.copyInput()
	go func() {
		_ = cmd.Wait()
		s.exited.Store(true)
		// Let the last output in, unless something Crush started still
		// holds the terminal open; the emulator closes only after.
		select {
		case <-outputDone:
		case <-time.After(outputDrain):
		}
		_ = tty.Close()
		<-outputDone
		_ = emu.Close()
		send(exitedMsg{id: id})
	}()
	return s, nil
}

func winsize(width, height int) *pty.Winsize {
	return &pty.Winsize{Cols: uint16(max(width, 1)), Rows: uint16(max(height, 1))}
}

// copyOutput feeds what Crush draws into the emulator.
func (s *session) copyOutput(send func(tea.Msg)) {
	buf := make([]byte, 32*1024)
	for {
		n, err := s.tty.Read(buf)
		if n > 0 {
			_, _ = s.emu.Write(buf[:n])
			if s.changed.Swap(false) {
				send(statusMsg{id: s.id})
			}
			if s.pictured.Swap(false) {
				s.mu.Lock()
				cmds := s.pictures
				s.pictures = nil
				s.mu.Unlock()
				send(picturesMsg{id: s.id, cmds: cmds})
			}
			if s.copied.Swap(false) {
				s.mu.Lock()
				text := s.clip
				s.mu.Unlock()
				send(clipboardMsg{text: text})
			}
			if s.visible.Load() && s.dirty.CompareAndSwap(false, true) {
				send(outputMsg{id: s.id})
			}
		}
		if err != nil {
			return
		}
	}
}

// copyInput passes keys, clicks and the emulator's answers to Crush's
// terminal queries on to Crush.
func (s *session) copyInput() {
	_, _ = io.Copy(s.tty, s.emu)
}

// setFocus tells Crush whether its window has focus, if it asked to know.
// The write waits for the input loop, so it doesn't hold up the host.
func (s *session) setFocus(focused bool) {
	if !s.focusEvents.Load() {
		return
	}
	seq := ansi.Blur
	if focused {
		seq = ansi.Focus
	}
	go func() { _, _ = io.WriteString(s.emu.InputPipe(), seq) }()
}

func (s *session) resize(width, height int) {
	s.emu.Resize(width, height)
	_ = pty.Setsize(s.tty, winsize(width, height))
}

func (s *session) snapshot() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

func (s *session) cursorState() (visible bool, shape vt.CursorStyle, blink bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cursor, s.shape, s.blink
}

// stop asks Crush to exit the way closing its terminal would, and kills it
// and everything it started if it hasn't ended after stopGrace.
func (s *session) stop() {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	s.stopped = true
	s.mu.Unlock()
	if s.exited.Load() || s.cmd == nil || s.cmd.Process == nil {
		return
	}
	if s.kind == terminalSession && s.tty != nil {
		// Closing the PTY gives the shell and foreground job the same
		// hangup as closing a normal terminal window.
		_ = s.tty.Close()
		// The close waits for the output loop's read, and interactive
		// shells ignore SIGTERM, so hang the shell up directly. It passes
		// the hangup on to its jobs.
		_ = s.cmd.Process.Signal(syscall.SIGHUP)
	}
	terminate(s.cmd.Process)
	time.AfterFunc(stopGrace, func() {
		if !s.exited.Load() {
			kill(s.cmd.Process)
		}
	})
}

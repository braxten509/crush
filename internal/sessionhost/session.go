package sessionhost

import (
	"bytes"
	"encoding/base64"
	"image/color"
	"io"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
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
)

// stopGrace is how long a closed session gets to save and exit before it
// is killed.
const stopGrace = 5 * time.Second

// outputDrain is how long an ended session's last output may take.
const outputDrain = 2 * time.Second

// session is one Crush process in its own hidden terminal.
type session struct {
	id  int
	emu *vt.SafeEmulator
	tty *os.File
	cmd *exec.Cmd

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
	copied      atomic.Bool

	mu      sync.Mutex
	status  Status
	cursor  bool
	shape   vt.CursorStyle
	blink   bool
	stopped bool
	clip    string // text Crush last copied, for the real clipboard

	// Kept by the host's update loop only.
	unread  bool // finished work the user hasn't looked at yet
	working bool // last reported state was working
}

// newSession returns a session's emulator, set up to follow what Crush
// reports through it; startSession then starts Crush.
func newSession(id, width, height int, fg, bg color.Color) *session {
	emu := vt.NewSafeEmulator(width, height)
	emu.SetDefaultForegroundColor(fg)
	emu.SetDefaultBackgroundColor(bg)
	s := &session{id: id, emu: emu, cursor: true}
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
		EnableMode: func(mode ansi.Mode) {
			if mode == ansi.ModeFocusEvent {
				s.focusEvents.Store(true)
			}
		},
		DisableMode: func(mode ansi.Mode) {
			if mode == ansi.ModeFocusEvent {
				s.focusEvents.Store(false)
			}
		},
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
	emu.RegisterOscHandler(statusOSC, func(data []byte) bool {
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

func startSession(id int, l launch, width, height int, fg, bg color.Color, send func(tea.Msg)) (*session, error) {
	s := newSession(id, width, height, fg, bg)
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
	terminate(s.cmd.Process)
	time.AfterFunc(stopGrace, func() {
		if !s.exited.Load() {
			kill(s.cmd.Process)
		}
	})
}

package backend

import (
	"sync"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/crush/internal/proto"
)

// Live `!` output (engine-design.md 5.9 a).

const (
	// shellOutputEvery is the shortest time between two output events of
	// one command.
	shellOutputEvery = 33 * time.Millisecond
	// shellOutputBurst is how much waiting output is sent at once, without
	// waiting for shellOutputEvery.
	shellOutputBurst = 16 << 10
)

// shellStream joins one command's output into shell_output events: at most
// one per shellOutputEvery, or at once when shellOutputBurst bytes wait.
// Each event holds whole UTF-8 characters: an incomplete one at the end of a
// chunk waits for the next chunk, or for the end of the command.
type shellStream struct {
	requestID, sessionID string
	send                 func(proto.ShellOutput)

	mu      sync.Mutex
	pending []byte // whole characters not sent yet
	tail    []byte // the first bytes of a character still arriving (at most 3)
	seq     int
	last    time.Time // when the previous event went
	timer   *time.Timer
	closed  bool
}

func newShellStream(requestID, sessionID string, send func(proto.ShellOutput)) *shellStream {
	return &shellStream{requestID: requestID, sessionID: sessionID, send: send}
}

// write takes a chunk of output as the command writes it.
func (s *shellStream) write(chunk string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	data := append(s.tail, chunk...)
	cut := wholeRunes(data)
	s.pending = append(s.pending, data[:cut]...)
	s.tail = append([]byte(nil), data[cut:]...)
	switch {
	case len(s.pending) == 0:
	case len(s.pending) >= shellOutputBurst:
		s.flushLocked()
	case s.timer != nil:
		// The waiting output goes when the timer fires.
	case time.Since(s.last) >= shellOutputEvery:
		s.flushLocked()
	default:
		s.timer = time.AfterFunc(shellOutputEvery-time.Since(s.last), s.tick)
	}
}

// tick sends what waited for the time limit.
func (s *shellStream) tick() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.timer = nil
	if !s.closed {
		s.flushLocked()
	}
}

// close sends what is left, the held-back bytes too, when the command ends.
func (s *shellStream) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.pending = append(s.pending, s.tail...)
	s.tail = nil
	s.flushLocked()
	s.closed = true
}

// flushLocked sends the waiting output as one event. Callers hold s.mu, so
// events leave in seq order.
func (s *shellStream) flushLocked() {
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	if len(s.pending) == 0 {
		return
	}
	s.send(proto.ShellOutput{RequestID: s.requestID, SessionID: s.sessionID, Seq: s.seq, Chunk: string(s.pending)})
	s.seq++
	s.pending = nil
	s.last = time.Now()
}

// wholeRunes is the length of b's longest prefix that does not end inside a
// UTF-8 character: an incomplete character at the end (at most 3 bytes) is
// left out. Invalid bytes count as whole.
func wholeRunes(b []byte) int {
	for i := len(b) - 1; i >= 0 && i > len(b)-utf8.UTFMax; i-- {
		if utf8.RuneStart(b[i]) {
			if utf8.FullRune(b[i:]) {
				return len(b)
			}
			return i
		}
	}
	return len(b)
}

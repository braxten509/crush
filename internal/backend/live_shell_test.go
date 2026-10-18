package backend

import (
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/crush/internal/proto"
	"github.com/stretchr/testify/require"
)

// shellRecorder collects a shellStream's events and when each went.
type shellRecorder struct {
	mu  sync.Mutex
	got []proto.ShellOutput
	at  []time.Time
}

func (r *shellRecorder) send(o proto.ShellOutput) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.got = append(r.got, o)
	r.at = append(r.at, time.Now())
}

func (r *shellRecorder) times() []time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]time.Time(nil), r.at...)
}

func (r *shellRecorder) events() []proto.ShellOutput {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]proto.ShellOutput(nil), r.got...)
}

// joined checks the events' order and returns their chunks joined.
func (r *shellRecorder) joined(t *testing.T) string {
	t.Helper()
	var sb strings.Builder
	for i, o := range r.events() {
		require.Equal(t, i, o.Seq, "events leave in seq order")
		require.Equal(t, "req-1", o.RequestID)
		require.Equal(t, "chat-1", o.SessionID)
		sb.WriteString(o.Chunk)
	}
	return sb.String()
}

// TestShellOutputSplitsOnRuneBoundaries: a character whose bytes arrive in
// two writes reaches the app whole, in one event (engine-design.md 5.9 a).
func TestShellOutputSplitsOnRuneBoundaries(t *testing.T) {
	t.Parallel()

	euro := "€"  // three bytes
	smile := "😀" // four bytes
	require.Len(t, euro, 3)
	require.Len(t, smile, 4)

	var rec shellRecorder
	s := newShellStream("req-1", "chat-1", rec.send)
	s.write("price: " + euro[:1])
	s.write(euro[1:] + " and " + smile[:2])
	s.write(smile[2:3])
	s.write(smile[3:] + "!")
	s.close()

	for _, o := range rec.events() {
		require.True(t, utf8.ValidString(o.Chunk), "chunk %d holds a broken character: %q", o.Seq, o.Chunk)
	}
	require.Equal(t, "price: "+euro+" and "+smile+"!", rec.joined(t))

	// The first write goes at once, without its held-back byte.
	require.Equal(t, "price: ", rec.events()[0].Chunk)
}

// TestShellOutputSplitsOnRuneBoundariesEndFlushes: bytes held back for a
// character that never completes still arrive when the command ends.
func TestShellOutputSplitsOnRuneBoundariesEndFlushes(t *testing.T) {
	t.Parallel()

	var rec shellRecorder
	s := newShellStream("req-1", "chat-1", rec.send)
	s.write("done" + "€"[:2])
	s.close()
	s.write("after the end")

	require.Equal(t, "done"+"€"[:2], rec.joined(t), "nothing is lost and nothing comes after the end")
}

// TestShellOutputCoalesces: events of one command are at least the interval
// apart, nothing waits forever, and a burst of 16 KiB goes at once.
func TestShellOutputCoalesces(t *testing.T) {
	t.Parallel()

	var rec shellRecorder
	s := newShellStream("req-1", "chat-1", rec.send)
	want := ""
	for i := range 40 {
		chunk := string(rune('a' + i%26))
		s.write(chunk)
		want += chunk
		if i%10 == 9 {
			time.Sleep(shellOutputEvery / 3)
		}
	}
	require.Eventually(t, func() bool {
		got := ""
		for _, o := range rec.events() {
			got += o.Chunk
		}
		return got == want
	}, 5*time.Second, 5*time.Millisecond, "the waiting output goes without a further write")
	at := rec.times()
	for i := 1; i < len(at); i++ {
		require.GreaterOrEqual(t, at[i].Sub(at[i-1]), shellOutputEvery, "events %d and %d", i-1, i)
	}

	n := len(rec.events())
	s.write(strings.Repeat("c", shellOutputBurst))
	require.Len(t, rec.events(), n+1, "a burst does not wait for the interval")
	s.close()
	require.Equal(t, want+strings.Repeat("c", shellOutputBurst), rec.joined(t))
}

func TestWholeRunes(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		in   string
		want int
	}{
		{"", 0},
		{"abc", 3},
		{"ab" + "€"[:1], 2},
		{"ab" + "€"[:2], 2},
		{"ab€", 5},
		{"😀"[:3], 0},
		{"😀", 4},
		{"a\x82\x82\x82", 4}, // invalid bytes count as whole
	} {
		require.Equal(t, tc.want, wholeRunes([]byte(tc.in)), "%q", tc.in)
	}
}

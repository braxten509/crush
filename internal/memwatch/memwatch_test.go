package memwatch

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const gib = 1 << 30

func TestParseMeminfo(t *testing.T) {
	t.Parallel()
	in := `MemTotal:       32727204 kB
MemFree:         4212884 kB
MemAvailable:   12584960 kB
Buffers:             120 kB
SwapTotal:      32727036 kB
SwapFree:       20711424 kB
`
	u, err := parseMeminfo(strings.NewReader(in))
	require.NoError(t, err)
	require.Equal(t, Usage{MemTotal: 32727204 << 10, MemAvailable: 12584960 << 10}, u)
	// 20142244 / 32727204; the 12 GB of swap in use doesn't count.
	require.Equal(t, 61, u.Percent())

	_, err = parseMeminfo(strings.NewReader("MemTotal: 10 kB\n"))
	require.Error(t, err)
}

func TestPercent(t *testing.T) {
	t.Parallel()
	require.Equal(t, 90, Usage{MemTotal: 10 * gib, MemAvailable: 1 * gib}.Percent())
	require.Equal(t, 0, Usage{}.Percent())
}

// Full zram swap with memory 72% full. Counting swap too read 86% and
// warned, though the compressed swap is already inside the 72%.
func TestPercentIgnoresSwap(t *testing.T) {
	t.Parallel()
	in := `MemTotal:       32727204 kB
MemAvailable:    9163617 kB
SwapTotal:      32727036 kB
SwapFree:              0 kB
`
	u, err := parseMeminfo(strings.NewReader(in))
	require.NoError(t, err)
	require.Equal(t, 72, u.Percent())
	require.Less(t, u.Percent(), WarnPercent)
}

// The night Crush was closed: 30.2 of 33.5 GB memory (and 32.8 of 33.5 GB
// swap). Memory alone must still warn.
func TestPercentAtTheCrash(t *testing.T) {
	t.Parallel()
	u := Usage{MemTotal: 33512656896, MemAvailable: 33512656896 - 30197383168}
	require.GreaterOrEqual(t, u.Percent(), WarnPercent)
}

func fakeWatcher(percents ...int) (*Watcher, *time.Time) {
	now := time.Unix(1000, 0)
	i := 0
	w := &Watcher{
		usage: func() (Usage, error) {
			p := percents[min(i, len(percents)-1)]
			i++
			if p < 0 {
				return Usage{}, errors.New("unreadable")
			}
			return Usage{MemTotal: 100 * gib, MemAvailable: uint64(100-p) * gib}, nil
		},
		top: func(n int) []Program { return []Program{{Name: "node", Bytes: 6 * gib}} },
		now: func() time.Time { return now },
	}
	return w, &now
}

func TestWarnAndClearLines(t *testing.T) {
	t.Parallel()
	w, _ := fakeWatcher(84, 85, 82, 80, 79, 90)
	var high []bool
	var episodes []int
	for range 6 {
		s := w.Check()
		high = append(high, s.High)
		episodes = append(episodes, s.Episode)
	}
	// Starts at 85, stays on above 80, ends below 80.
	require.Equal(t, []bool{false, true, true, true, false, true}, high)
	require.Equal(t, []int{0, 1, 1, 1, 1, 2}, episodes, "a new episode each time it gets high")
}

func TestTopOnlyWhileHigh(t *testing.T) {
	t.Parallel()
	w, _ := fakeWatcher(50, 88)
	require.Empty(t, w.Check().Top)
	require.Equal(t, []Program{{Name: "node", Bytes: 6 * gib}}, w.Check().Top)
}

func TestUnreadableKeepsLastStatus(t *testing.T) {
	t.Parallel()
	w, _ := fakeWatcher(88, -1)
	first := w.Check()
	require.Equal(t, first, w.Check())
}

func TestRecentReusesFreshCheck(t *testing.T) {
	t.Parallel()
	w, now := fakeWatcher(88, 50)
	require.True(t, w.Recent(CheckEvery).High)
	*now = now.Add(5 * time.Second)
	require.True(t, w.Recent(CheckEvery).High, "a 5 s old check is reused")
	*now = now.Add(10 * time.Second)
	require.False(t, w.Recent(CheckEvery).High, "an old check is redone")
}

func TestBiggestAddsUpByName(t *testing.T) {
	t.Parallel()
	got := biggest([]Program{
		{"zen", 1 * gib}, {"qemu", 5 * gib}, {"zen", 2 * gib},
		{"zen", 3 * gib}, {"bash", 1 << 20}, {"claude", 1 * gib},
	}, 3)
	require.Equal(t, []Program{{"zen", 6 * gib}, {"qemu", 5 * gib}, {"claude", 1 * gib}}, got)
}

func TestMessages(t *testing.T) {
	t.Parallel()
	s := Status{Percent: 88, High: true, Top: []Program{{"node", 6*gib + gib/10}, {"bash", 300 << 20}}}
	require.Equal(t, "Memory 88% full. Biggest: node 6.1 GB, bash 300 MB. Close something, or the system may close Crush.", s.Message())

	note := s.AgentNote()
	require.Contains(t, note, "88% full")
	require.Contains(t, note, "do not start big tests, builds")
	require.Contains(t, note, "node (6.1 GB)")
	require.Contains(t, note, "Start your reply by telling the user")
	require.Equal(t, "Memory 88% full: the AI was asked to hold off on big tests and builds", s.ChatNotice())

	require.Empty(t, Status{Percent: 88}.AgentNote(), "no note unless High")
}

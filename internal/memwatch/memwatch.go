// Package memwatch warns before the system runs out of memory.
//
// When memory and swap fill up, systemd-oomd (or the kernel) stops the
// biggest program, and the terminal tab running Crush, together with every
// agent and job it started, is usually the biggest. Crush cannot prevent
// that, but it can tell the user and its agents early enough to free memory.
package memwatch

import (
	"bufio"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// WarnPercent is how full memory must be before Crush warns.
	// systemd-oomd acts when memory and swap are both over 90%.
	WarnPercent = 85
	// ClearPercent is where the warning ends. It is lower than
	// WarnPercent so a reading hovering at the line doesn't flicker.
	ClearPercent = 80
	// CheckEvery is how often the UI checks memory.
	CheckEvery = 10 * time.Second
	// NotifyEvery is the shortest time between two desktop pop-ups across
	// every Crush window.
	NotifyEvery = 10 * time.Minute
	// topCount is how many of the biggest programs a warning names.
	topCount = 3
)

// Usage is how much memory is in use, in bytes.
//
// Swap is left out on purpose. With zram (CachyOS and Fedora's default),
// swapped pages stay in memory, compressed, and that compressed size is
// already part of MemTotal - MemAvailable; adding swap on top counts the
// same pages twice. systemd-oomd and the kernel only close programs once
// memory itself is nearly full, so memory alone still warns first.
type Usage struct {
	MemTotal, MemAvailable uint64
}

// Percent is how full memory is, 0 to 100.
func (u Usage) Percent() int {
	if u.MemTotal == 0 {
		return 0
	}
	return int(sub(u.MemTotal, u.MemAvailable) * 100 / u.MemTotal)
}

func sub(a, b uint64) uint64 {
	if b > a {
		return 0
	}
	return a - b
}

// Program is one program and the memory it holds in RAM and swap. Every
// process with the same name counts as one program.
type Program struct {
	Name  string
	Bytes uint64
}

// Status is the result of one check.
type Status struct {
	Percent int
	// High is true from the moment Percent reaches WarnPercent until it
	// drops below ClearPercent.
	High bool
	// Episode counts the times memory got high, so a notice can be shown
	// once per episode.
	Episode int
	// Top lists the biggest programs, only while High.
	Top []Program
}

// Message is the one-line warning shown to the user.
func (s Status) Message() string {
	msg := fmt.Sprintf("Memory %d%% full.", s.Percent)
	if len(s.Top) > 0 {
		parts := make([]string, len(s.Top))
		for i, p := range s.Top {
			parts[i] = p.Name + " " + formatBytes(p.Bytes)
		}
		msg += " Biggest: " + strings.Join(parts, ", ") + "."
	}
	return msg + " Close something, or the system may close Crush."
}

// AgentNote is the note added to an agent's turn while memory is high, or
// "" when it is not.
func (s Status) AgentNote() string {
	if !s.High {
		return ""
	}
	note := fmt.Sprintf("<crush_memory_warning>\nThe computer's memory is %d%% full. When it fills up, the system closes the biggest program, which can be Crush with every chat and job in it. Until this warning is gone, do not start big tests, builds, emulators, browsers or many sub-agents; prefer small, targeted checks, and stop background jobs you no longer need.", s.Percent)
	if len(s.Top) > 0 {
		parts := make([]string, len(s.Top))
		for i, p := range s.Top {
			parts[i] = p.Name + " (" + formatBytes(p.Bytes) + ")"
		}
		note += " Biggest programs right now: " + strings.Join(parts, ", ") + "."
	}
	note += " Start your reply by telling the user in one short sentence that memory is almost full and what you are holding off on. Keep doing the smaller parts of the work; don't stop and wait unless the next step needs a big test or build."
	return note + "\n</crush_memory_warning>"
}

// ChatNotice is the line shown in a chat whose turn got the AgentNote.
func (s Status) ChatNotice() string {
	return fmt.Sprintf("Memory %d%% full: the AI was asked to hold off on big tests and builds", s.Percent)
}

func formatBytes(b uint64) string {
	gb := float64(b) / (1 << 30)
	if gb >= 1 {
		return strconv.FormatFloat(gb, 'f', 1, 64) + " GB"
	}
	return strconv.FormatUint(b>>20, 10) + " MB"
}

// Watcher keeps the warning state between checks.
type Watcher struct {
	usage func() (Usage, error)
	top   func(n int) []Program
	now   func() time.Time

	mu      sync.Mutex
	high    bool
	episode int
	last    Status
	at      time.Time
}

// Default watches this computer. Readings are unavailable outside Linux,
// where checks always report a calm Status.
var Default = &Watcher{usage: readUsage, top: topPrograms, now: time.Now}

// Check reads memory now and updates the warning state.
func (w *Watcher) Check() Status {
	u, err := w.usage()
	w.mu.Lock()
	defer w.mu.Unlock()
	if err != nil {
		return w.last
	}
	s := Status{Percent: u.Percent()}
	switch {
	case s.Percent >= WarnPercent:
		if !w.high {
			w.episode++
		}
		w.high = true
	case s.Percent < ClearPercent:
		w.high = false
	}
	s.High, s.Episode = w.high, w.episode
	if s.High {
		s.Top = w.top(topCount)
	}
	w.last, w.at = s, w.now()
	return s
}

// Recent returns the last check if it is younger than maxAge, and checks
// again otherwise.
func (w *Watcher) Recent(maxAge time.Duration) Status {
	w.mu.Lock()
	if !w.at.IsZero() && w.now().Sub(w.at) < maxAge {
		s := w.last
		w.mu.Unlock()
		return s
	}
	w.mu.Unlock()
	return w.Check()
}

// parseMeminfo reads the fields Usage needs from /proc/meminfo.
func parseMeminfo(r io.Reader) (Usage, error) {
	var u Usage
	fields := map[string]*uint64{
		"MemTotal":     &u.MemTotal,
		"MemAvailable": &u.MemAvailable,
	}
	found := 0
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		name, rest, ok := strings.Cut(sc.Text(), ":")
		dst, want := fields[name]
		if !ok || !want {
			continue
		}
		kb, err := parseKB(rest)
		if err != nil {
			return Usage{}, fmt.Errorf("meminfo %s: %w", name, err)
		}
		*dst = kb
		found++
	}
	if err := sc.Err(); err != nil {
		return Usage{}, err
	}
	if u.MemTotal == 0 || found < len(fields) {
		return Usage{}, fmt.Errorf("meminfo: missing fields")
	}
	return u, nil
}

// parseKB turns "  1234 kB" into bytes.
func parseKB(s string) (uint64, error) {
	f := strings.Fields(s)
	if len(f) == 0 {
		return 0, fmt.Errorf("empty value")
	}
	n, err := strconv.ParseUint(f[0], 10, 64)
	if err != nil {
		return 0, err
	}
	return n << 10, nil
}

// biggest adds up memory per program name and returns the n largest.
func biggest(procs []Program, n int) []Program {
	sum := map[string]uint64{}
	for _, p := range procs {
		sum[p.Name] += p.Bytes
	}
	out := make([]Program, 0, len(sum))
	for name, b := range sum {
		out = append(out, Program{Name: name, Bytes: b})
	}
	slices.SortFunc(out, func(a, b Program) int {
		if a.Bytes != b.Bytes {
			if a.Bytes > b.Bytes {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Name, b.Name)
	})
	return out[:min(n, len(out))]
}

// TotalRAM is the computer's RAM in bytes, or 0 where it can't be read.
func TotalRAM() uint64 {
	Default.mu.Lock()
	read := Default.usage
	Default.mu.Unlock()
	u, err := read()
	if err != nil {
		return 0
	}
	return u.MemTotal
}

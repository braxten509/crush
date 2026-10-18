package backend

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/memlimit"
	"github.com/charmbracelet/crush/internal/memwatch"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/pubsub"
)

// Memory warning and memory box (engine-design.md 5.9 c).

// memoryChecker reads the computer's memory; memwatch.Default does.
type memoryChecker interface {
	Check() memwatch.Status
	Recent(maxAge time.Duration) memwatch.Status
}

// memorySource is the memory reader and the check interval. Tests replace
// them (setMemorySource) while other engines' checks may run, hence the
// lock.
var memorySource = struct {
	sync.Mutex
	watcher memoryChecker
	every   time.Duration
}{watcher: memwatch.Default, every: memwatch.CheckEvery}

// memoryReader returns the memory reader and the check interval.
func memoryReader() (memoryChecker, time.Duration) {
	memorySource.Lock()
	defer memorySource.Unlock()
	return memorySource.watcher, memorySource.every
}

// setMemorySource makes w the memory reader and every the check interval
// until restore runs.
func setMemorySource(w memoryChecker, every time.Duration) (restore func()) {
	memorySource.Lock()
	defer memorySource.Unlock()
	savedW, savedEvery := memorySource.watcher, memorySource.every
	memorySource.watcher, memorySource.every = w, every
	return func() {
		memorySource.Lock()
		defer memorySource.Unlock()
		memorySource.watcher, memorySource.every = savedW, savedEvery
	}
}

// totalRAM is the computer's RAM size.
var totalRAM = memwatch.TotalRAM

// memoryLimitInputError is a limit the dialog refuses. Its text is the
// dialog's, and it is ErrMemoryLimitInvalid.
type memoryLimitInputError struct{ text string }

func (e memoryLimitInputError) Error() string      { return e.text }
func (memoryLimitInputError) Is(target error) bool { return target == ErrMemoryLimitInvalid }

// MemoryStatus is the computer's memory use, as the last check (at most
// memwatch.CheckEvery old) saw it, and the memory box's limit.
func (b *Backend) MemoryStatus() proto.MemoryStatus {
	w, _ := memoryReader()
	return b.memoryStatus(w.Recent(memwatch.CheckEvery))
}

func (b *Backend) memoryStatus(st memwatch.Status) proto.MemoryStatus {
	ram := totalRAM()
	out := proto.MemoryStatus{
		Supported:     ram > 0,
		Percent:       st.Percent,
		High:          st.High,
		Episode:       st.Episode,
		LimitBytes:    b.memoryLimitBytes(ram),
		TotalRAMBytes: ram,
	}
	if used, ok := memlimit.Usage(memlimit.Slice); ok {
		out.BoxUsedBytes = used
	}
	if st.High {
		for _, p := range st.Top {
			out.Top = append(out.Top, proto.MemoryProgram{Name: p.Name, Bytes: p.Bytes})
		}
		out.Message = st.Message()
	}
	return out
}

// memoryLimitBytes is the box's limit as the memory-limit dialog shows it:
// the saved limit, else the configured one, else half of the RAM.
func (b *Backend) memoryLimitBytes(ram uint64) uint64 {
	if gb, ok := config.SavedMemoryLimitGB(); ok {
		return (&config.Options{MemoryLimitGB: &gb}).MemoryLimitBytes(ram)
	}
	var opts *config.Options
	if b.cfg != nil {
		if cfg := b.cfg.Config(); cfg != nil {
			opts = cfg.Options
		}
	}
	return opts.MemoryLimitBytes(ram)
}

// ClaimMemoryNotice claims the memory pop-up: one shows per 10 minutes
// across every Crush window, terminal or app.
func (b *Backend) ClaimMemoryNotice() proto.ClaimResult {
	return proto.ClaimResult{Claimed: memwatch.ClaimNotify(config.GlobalCacheDir())}
}

// SetMemoryLimit parses, saves and applies a limit as the terminal's
// dialog does, with its outcome text. A refused value is
// ErrMemoryLimitInvalid with the dialog's validation text.
func (b *Backend) SetMemoryLimit(value string) (proto.MemoryLimitResult, error) {
	gb, err := config.ParseMemoryLimit(value)
	if err != nil {
		return proto.MemoryLimitResult{}, memoryLimitInputError{text: err.Error()}
	}
	limit, applyErr, err := config.SaveMemoryLimit(value)
	if err != nil {
		return proto.MemoryLimitResult{}, err
	}
	res := proto.MemoryLimitResult{LimitBytes: limit, Applied: applyErr == nil}
	switch {
	case applyErr != nil:
		res.Notice = fmt.Errorf("saved, but couldn't apply the memory limit: %w", applyErr).Error()
	case gb == 0:
		res.Notice = "Memory limit removed"
	default:
		res.Notice = fmt.Sprintf("Memory limit set to %g GB for every Crush window", gb)
	}
	return res, nil
}

// startMemoryChecks runs a memory check every check interval while the
// engine has a workspace. It also keeps the agent's memory note current.
func (b *Backend) startMemoryChecks() {
	e := b.liveEngine()
	e.memMu.Lock()
	defer e.memMu.Unlock()
	if e.memStop != nil {
		return
	}
	ctx, cancel := context.WithCancel(b.engineContext())
	e.memStop = cancel
	_, every := memoryReader()
	go func() {
		ticker := time.NewTicker(every)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				b.checkMemory()
			}
		}
	}()
}

// stopMemoryChecksIfIdle stops the memory checks once no workspace is
// left. A workspace created meanwhile restarts them in its joined hook.
func (b *Backend) stopMemoryChecksIfIdle() {
	e := b.liveEngine()
	e.memMu.Lock()
	defer e.memMu.Unlock()
	if e.memStop == nil || b.workspaces.Len() > 0 {
		return
	}
	e.memStop()
	e.memStop = nil
}

// checkMemory runs one check. When the percent, the high mark or the
// episode changed since the previous check, every app gets memory_status.
// The first check only sets the baseline: apps read the route on connect.
func (b *Backend) checkMemory() {
	e := b.liveEngine()
	e.memCheck.Lock()
	defer e.memCheck.Unlock()
	w, _ := memoryReader()
	st := w.Check()
	prev, seen := e.memLast, e.memSeen
	e.memLast, e.memSeen = st, true
	if !seen || st.Percent == prev.Percent && st.High == prev.High && st.Episode == prev.Episode {
		return
	}
	ev := EngineEvent{Type: pubsub.PayloadTypeMemoryStatus, Kind: pubsub.UpdatedEvent, Payload: b.memoryStatus(st)}
	for _, ws := range b.workspaces.Seq2() {
		b.SendAll(ws.ID, ev)
	}
}

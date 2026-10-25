package message

import (
	"context"
	"database/sql"
	"time"

	"github.com/charmbracelet/crush/internal/db"
	"github.com/charmbracelet/crush/internal/pubsub"
)

// pendingState holds the in-memory coalescing buffer for a single
// message ID. All fields except where noted are guarded by
// service.mu. The flushing flag serializes concurrent flushers for
// the same ID so SQL writes never reorder.
type pendingState struct {
	// latest is the most recent [Message] passed to [Service.Update]
	// that has not yet been flushed.
	latest Message

	// dirty is true when latest contains state that has not been
	// written to SQL since the last successful flush.
	dirty bool

	// flushing is true while a goroutine is performing the SQL write
	// for this ID. New updates are still accepted (and re-mark dirty)
	// but other flushers must back off.
	flushing bool

	// timer is the active debounce timer, or nil if no flush is
	// scheduled. Stopped and reset when a terminal update preempts
	// the debounce window.
	timer *time.Timer

	// baseline is the projection of the snapshot most recently
	// written to SQL that terminal-state detection needs. Used as the
	// baseline for [shouldFlushNow].
	baseline flushBaseline

	// hasFlushed is false until the first successful write for this
	// ID; until then baseline is the zero value and must not be
	// treated as a real prior state.
	hasFlushed bool
}

// flushBaseline is the compact projection of a flushed [Message] that
// [shouldFlushNow] compares against. Pending state lives for as long
// as the process does, so it retains this instead of the whole
// Message: a finished stream would otherwise pin its parts — reasoning
// text, tool inputs, tool results — on the heap forever.
type flushBaseline struct {
	compacting          bool
	toolCallsFinished   []bool
	reasoningFinishedAt int64
}

// newFlushBaseline projects the fields [shouldFlushNow] reads out of a
// flushed message.
func newFlushBaseline(m *Message) flushBaseline {
	calls := m.ToolCalls()
	finished := make([]bool, len(calls))
	for i, c := range calls {
		finished[i] = c.Finished
	}
	return flushBaseline{
		compacting:          m.IsCompacting,
		toolCallsFinished:   finished,
		reasoningFinishedAt: m.ReasoningContent().FinishedAt,
	}
}

// Update accepts a new state for a message and either flushes
// synchronously (terminal updates, debounce <= 0) or buffers it until
// the next debounce tick. See [Service] for the contract.
func (s *service) Update(ctx context.Context, msg Message) error {
	cloned := msg.Clone()

	// Zero or negative debounce: flush every update synchronously. This
	// preserves the pre-coalescing behaviour for tests and any caller
	// that explicitly opted out via [WithDebounce].
	if s.debounce <= 0 {
		s.mu.Lock()
		p, ok := s.pending[msg.ID]
		if !ok {
			p = &pendingState{}
			s.pending[msg.ID] = p
		}
		p.latest = cloned
		p.dirty = true
		s.mu.Unlock()
		return s.flushOne(ctx, msg.ID, true)
	}

	s.mu.Lock()
	p, ok := s.pending[msg.ID]
	if !ok {
		p = &pendingState{}
		s.pending[msg.ID] = p
	}
	p.latest = cloned
	p.dirty = true

	var prev *flushBaseline
	if p.hasFlushed {
		prev = &p.baseline
	}
	terminal := shouldFlushNow(prev, &cloned)

	if terminal {
		if p.timer != nil {
			p.timer.Stop()
			p.timer = nil
		}
		s.mu.Unlock()
		return s.flushOne(ctx, msg.ID, true)
	}

	// Debounce: schedule a single flush per pending state. If a flush
	// is already running we let it finish; it schedules the next one for
	// the trailing dirty bit.
	s.scheduleFlushLocked(p, msg.ID)
	s.mu.Unlock()
	return nil
}

// scheduleFlushLocked starts the debounce timer for id unless one is set or
// a flush is running. Caller must hold s.mu.
func (s *service) scheduleFlushLocked(p *pendingState, id string) {
	if p.timer != nil || p.flushing {
		return
	}
	p.timer = time.AfterFunc(s.debounce, func() {
		// Detached from caller ctx so a cancelled stream context
		// does not strand the buffered write.
		_ = s.flushOne(context.Background(), id, false)
	})
}

// Flush implements [Service.Flush].
func (s *service) Flush(ctx context.Context, id string) error {
	return s.flushOne(ctx, id, true)
}

// FlushAll implements [Service.FlushAll]. It snapshots every ID with
// outstanding work — either dirty buffered state or a flush already in
// flight — then drains each one. Picking up in-flight IDs ensures
// FlushAll cannot return while a timer-fired write is still mid-SQL,
// which is what shutdown and session-switch callers rely on.
func (s *service) FlushAll(ctx context.Context) error {
	s.mu.Lock()
	ids := make([]string, 0, len(s.pending))
	for id, p := range s.pending {
		if p.dirty || p.flushing {
			ids = append(ids, id)
		}
	}
	s.mu.Unlock()
	var firstErr error
	for _, id := range ids {
		if err := s.flushOne(ctx, id, true); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// flushOne drains a single message ID. When syncCaller is true the
// caller is willing to wait through a concurrent in-flight flush so
// that, on return, the flushed state equals latest at the moment of
// return.
// When false (timer-fired path) we bail if another flusher is already
// running; that flusher will pick up the trailing dirty bit.
//
// Order matters: a sync caller must wait for any in-flight flush to
// drain even when the buffer is currently clean — that in-flight
// write has not yet updated the SQL row, so returning early would
// violate the contract that on success the flushed state reflects the
// most recent state.
func (s *service) flushOne(ctx context.Context, id string, syncCaller bool) error {
	for {
		s.mu.Lock()
		p, ok := s.pending[id]
		if !ok {
			s.mu.Unlock()
			return nil
		}
		if p.flushing {
			if !syncCaller {
				s.mu.Unlock()
				return nil
			}
			s.mu.Unlock()
			// Brief yield; in-flight write should land in <1ms typical.
			time.Sleep(time.Millisecond)
			continue
		}
		if !p.dirty {
			s.mu.Unlock()
			return nil
		}

		if p.timer != nil {
			p.timer.Stop()
			p.timer = nil
		}
		snap := p.latest
		// Decide whether this snapshot represents a terminal event
		// against the prior baseline. We must do this before resetting
		// dirty/flushing because shouldFlushNow looks at p.baseline
		// (which is what was on disk before this write).
		var prev *flushBaseline
		if p.hasFlushed {
			prev = &p.baseline
		}
		isTerminal := shouldFlushNow(prev, &snap)
		p.flushing = true
		p.dirty = false
		s.mu.Unlock()

		written, err := s.write(ctx, snap)
		if err == nil {
			snap = written
		}

		s.mu.Lock()
		p.flushing = false
		if err == nil {
			p.baseline = newFlushBaseline(&snap)
			p.hasFlushed = true
		} else {
			// Restore dirty so the next caller retries.
			p.dirty = true
		}
		// If a delta arrived during the SQL write and we are a sync
		// caller, the user expects that delta to land too.
		wasDirty := p.dirty
		if !wasDirty {
			// Nothing left to write, so drop the snapshot. Pending
			// entries outlive the streams that created them; holding
			// latest here would pin every finished message's parts
			// for the life of the process.
			p.latest = Message{}
		} else if err == nil && !syncCaller && s.debounce > 0 {
			// A delta arrived during this timer-fired write: nothing
			// else may come to write it, so schedule the next flush.
			s.scheduleFlushLocked(p, id)
		}
		s.mu.Unlock()

		if err != nil {
			return err
		}

		// Terminal events — message finished, tool call added or
		// finished, reasoning ended — use the bounded must-deliver
		// path so they never get dropped under channel contention.
		if isTerminal {
			s.PublishMustDeliver(ctx, pubsub.UpdatedEvent, snap)
		} else {
			s.Publish(pubsub.UpdatedEvent, snap)
		}

		if wasDirty && syncCaller {
			continue
		}
		return nil
	}
}

// write performs the unguarded SQL write + UpdatedAt stamp. Caller
// owns publishing.
func (s *service) write(ctx context.Context, msg Message) (Message, error) {
	s.captureFiles(ctx, msg.ID, msg.SessionID, msg.Parts)
	var payload []byte
	storage, separate := s.q.(reviewStorage)
	if separate {
		var err error
		msg.Parts, payload, err = separateReviews(msg.ID, msg.SessionID, msg.Parts)
		if err != nil {
			return Message{}, err
		}
	}
	parts, err := marshalParts(msg.Parts)
	if err != nil {
		return Message{}, err
	}
	finishedAt := sql.NullInt64{}
	if f := msg.FinishPart(); f != nil {
		finishedAt.Int64 = f.Time
		finishedAt.Valid = true
	}
	arg := db.UpdateMessageParams{
		ID:                      msg.ID,
		Parts:                   string(parts),
		PrismModelID:            sql.NullString{String: msg.PrismModelID, Valid: msg.PrismModelID != ""},
		PrismModelName:          sql.NullString{String: msg.PrismModelName, Valid: msg.PrismModelName != ""},
		PrismHypercreditSavings: nullableFloat(msg.PrismHypercreditSavings),
		PrismDollarSavings:      nullableFloat(msg.PrismDollarSavings),
		FinishedAt:              finishedAt,
	}
	if separate && len(payload) > 0 {
		err = storage.UpdateMessageWithReview(ctx, arg, payload)
	} else {
		err = s.q.UpdateMessage(ctx, arg)
	}
	if err != nil {
		return Message{}, err
	}
	return msg, nil
}

func nullableFloat(v *float64) sql.NullFloat64 {
	if v == nil {
		return sql.NullFloat64{}
	}
	return sql.NullFloat64{Float64: *v, Valid: true}
}

func floatPtr(v sql.NullFloat64) *float64 {
	if !v.Valid {
		return nil
	}
	value := v.Float64
	return &value
}

// shouldFlushNow returns true when next represents a structural
// change that must not be silently coalesced: the message just
// finished, the tool-call set grew, a tool call transitioned to
// finished, or reasoning just finished. prev is the last-flushed
// snapshot (or nil if no write has landed yet).
func shouldFlushNow(prev *flushBaseline, next *Message) bool {
	if next.IsFinished() {
		return true
	}

	if (prev != nil && prev.compacting != next.IsCompacting) || (prev == nil && next.IsCompacting) {
		return true
	}

	var prevCalls []bool
	var prevReasoningFinishedAt int64
	if prev != nil {
		prevCalls = prev.toolCallsFinished
		prevReasoningFinishedAt = prev.reasoningFinishedAt
	}
	nextCalls := next.ToolCalls()
	if len(nextCalls) != len(prevCalls) {
		return true
	}
	for i := range nextCalls {
		// Bounds-safe: lengths are equal here.
		if nextCalls[i].Finished != prevCalls[i] {
			return true
		}
		// A tool call's input only matters once it has landed (Finished
		// flips true). Earlier deltas to Input are debounced with the
		// rest of the streaming state.
	}
	if next.ReasoningContent().FinishedAt > 0 && prevReasoningFinishedAt == 0 {
		return true
	}
	return false
}

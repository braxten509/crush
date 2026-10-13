package cliagent

import (
	"sync"
	"sync/atomic"
	"time"
)

// liveProc is an agent CLI process kept open between the turns of one
// Crush session, as the interactive CLIs do, so follow-ups skip startup
// and commands the CLI moved to the background keep running. K is what
// the process was started with; a turn that needs anything else starts a
// new one.
type liveProc[K comparable] struct {
	p     *proc
	lines <-chan []byte
	key   K
	// idle is closed to end the between-turns drain, which closes drained
	// once it stopped reading lines.
	idle    chan struct{}
	drained chan struct{}
	dead    atomic.Bool
	timer   *time.Timer
}

func (l *liveProc[K]) live() *liveProc[K] { return l }

// stop ends the between-turns drain and the process.
func (l *liveProc[K]) stop() {
	l.timer.Stop()
	close(l.idle)
	<-l.drained
	l.p.finish()
}

// parkable is a kept process of one agent CLI.
type parkable[K comparable] interface {
	comparable
	live() *liveProc[K]
	// resumeID is the CLI's ID for the conversation the process holds; a
	// turn resumes the process only to continue that conversation.
	resumeID() string
}

// livePool holds the live processes of one agent CLI, one per Crush
// session.
type livePool[K comparable, L parkable[K]] struct {
	mu sync.Mutex
	m  map[string]L
}

func newLivePool[K comparable, L parkable[K]]() *livePool[K, L] {
	return &livePool[K, L]{m: map[string]L{}}
}

// take hands over the session's live process if it can run a turn started
// with key that resumes resume, closing it otherwise. It returns the zero
// L when there is none to hand over.
func (pool *livePool[K, L]) take(sessionID string, key K, resume string) L {
	var none L
	pool.mu.Lock()
	l, ok := pool.m[sessionID]
	delete(pool.m, sessionID)
	pool.mu.Unlock()
	if !ok {
		return none
	}
	base := l.live()
	base.timer.Stop()
	close(base.idle)
	<-base.drained
	if base.dead.Load() || base.key != key || resume == "" || resume != l.resumeID() {
		base.p.finish()
		return none
	}
	return l
}

// keep parks a process after a finished turn until the session's next
// prompt, or closes it after [claudeIdle]. Until then it drains the
// process's output: onLine, when set, sees each line and returns true to
// stop reading and leave the rest for the next turn. busy, when set, keeps
// the process open past the idle timeout while it reports work running.
func (pool *livePool[K, L]) keep(sessionID string, l L, onLine func([]byte) bool, busy func() bool) {
	base := l.live()
	base.idle, base.drained = make(chan struct{}), make(chan struct{})
	go func() {
		defer close(base.drained)
		for {
			select {
			case <-base.idle:
				return
			case raw, ok := <-base.lines:
				if !ok {
					base.dead.Store(true)
					<-base.idle
					return
				}
				if onLine != nil && onLine(raw) {
					<-base.idle
					return
				}
			}
		}
	}()
	base.timer = time.AfterFunc(claudeIdle, func() {
		pool.mu.Lock()
		if pool.m[sessionID] != l {
			pool.mu.Unlock()
			return
		}
		if busy != nil && busy() && !base.dead.Load() {
			base.timer.Reset(claudeIdle) // The CLI reports the work when it ends.
			pool.mu.Unlock()
			return
		}
		delete(pool.m, sessionID)
		pool.mu.Unlock()
		close(base.idle)
		<-base.drained
		base.p.finish()
	})
	pool.mu.Lock()
	old, hadOld := pool.m[sessionID]
	pool.m[sessionID] = l
	pool.mu.Unlock()
	if hadOld {
		old.live().stop()
	}
}

// close closes the session's parked process, unless refuse returns an
// error for it, which close then returns.
func (pool *livePool[K, L]) close(sessionID string, refuse func(L) error) error {
	pool.mu.Lock()
	l, ok := pool.m[sessionID]
	if ok && refuse != nil {
		if err := refuse(l); err != nil {
			pool.mu.Unlock()
			return err
		}
	}
	if ok {
		delete(pool.m, sessionID)
	}
	pool.mu.Unlock()
	if ok {
		l.live().stop()
	}
	return nil
}

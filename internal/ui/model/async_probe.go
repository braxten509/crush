package model

import "time"

// asyncProbe memoizes one piece of workspace state that is fetched off the
// Update goroutine (each fetch is a synchronous HTTP round-trip in
// client/server mode), with the bookkeeping that keeps at most one fetch in
// flight and still lands the freshest state:
//
//   - request asks for a refresh; while a fetch is in flight it only queues
//     one, so events arriving in bursts cost a single extra fetch.
//   - start marks a fetch in flight and stamps the check time at dispatch,
//     so the TTL backstop doesn't keep re-requesting meanwhile.
//   - finish records that a fetch landed and reports whether a refresh was
//     queued in the meantime, which the caller then dispatches.
//
// All methods run on the Update goroutine.
type asyncProbe[T any] struct {
	// val is the last fetched state.
	val T
	// inFlight is set while a fetch is running.
	inFlight bool
	// queued records that a refresh was requested while a fetch was
	// already in flight.
	queued bool
	// checkedAt is when the last fetch was dispatched or landed.
	checkedAt time.Time
}

// request reports whether a refresh may be dispatched now. While a fetch is
// in flight it queues one instead.
func (p *asyncProbe[T]) request() bool {
	if p.inFlight {
		p.queued = true
		return false
	}
	return true
}

// start marks a fetch in flight. It reports false, changing nothing, while
// one already is.
func (p *asyncProbe[T]) start() bool {
	if p.inFlight {
		return false
	}
	p.inFlight = true
	p.checkedAt = time.Now()
	return true
}

// finish records that the in-flight fetch landed and reports whether a
// refresh was requested meanwhile; the caller dispatches it.
func (p *asyncProbe[T]) finish() (requeue bool) {
	p.inFlight = false
	p.checkedAt = time.Now()
	requeue = p.queued
	p.queued = false
	return requeue
}

// stale reports whether the state has gone ttl without a check.
func (p *asyncProbe[T]) stale(ttl time.Duration) bool {
	return time.Since(p.checkedAt) >= ttl
}

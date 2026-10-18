package workspace

import (
	"context"
	"time"
)

// ensureEngineTimeout bounds one run of the ensure function after the
// engine was lost. A var, not a const, so tests can shrink it.
var ensureEngineTimeout = 15 * time.Second

// maxEnsureFailures is the number of failed ensures in a row after which
// the connection is reported stuck. The loop keeps trying.
const maxEnsureFailures = 3

// SetEnsureEngine sets the function that starts the project's engine again
// when it cannot be reached (engine-design.md 5.14). Only engine
// connections set it: the subscription loop then runs it when the engine
// is unreachable, and the fresh engine's 404 leads to the workspace being
// created again. Call it before [ClientWorkspace.Subscribe].
func (w *ClientWorkspace) SetEnsureEngine(fn func(context.Context) error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.ensureEngine = fn
}

// ensureEngineFunc returns the ensure function, or nil when this is not an
// engine connection.
func (w *ClientWorkspace) ensureEngineFunc() func(context.Context) error {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.ensureEngine
}

// runEnsureEngine runs fn with its time limit, ending early when the
// workspace shuts down.
func (w *ClientWorkspace) runEnsureEngine(fn func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(w.subCtx, ensureEngineTimeout)
	defer cancel()
	return fn(ctx)
}

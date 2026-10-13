package backend

import (
	"log/slog"
	"time"
)

// teardown removes the workspace from the index, shuts down its
// underlying [app.App], and triggers a server shutdown if it was the
// last workspace alive.
//
// Callers reach teardown after observing len(ws.clients) == 0 while
// holding ws.clientsMu and then releasing it. Between that release
// and the b.mu.Lock below, a concurrent CreateWorkspace may have
// re-registered a client (CreateWorkspace holds b.mu while doing so,
// so it is mutually exclusive with this critical section). teardown
// re-checks under both locks (in the canonical b.mu -> ws.clientsMu
// order) and aborts if the workspace has been re-claimed.
func (b *Backend) teardown(ws *Workspace) {
	b.mu.Lock()
	ws.clientsMu.Lock()
	if len(ws.clients) > 0 {
		// Race: a CreateWorkspace re-registered a client
		// between the detach path dropping ws.clientsMu and us
		// taking b.mu. Abort: the workspace is still alive.
		ws.clientsMu.Unlock()
		b.mu.Unlock()
		return
	}
	ws.clientsMu.Unlock()
	if existing, ok := b.pathIndex[ws.resolvedPath]; ok && existing == ws.ID {
		delete(b.pathIndex, ws.resolvedPath)
	}
	b.workspaces.Del(ws.ID)
	// Arm (or, with lingering disabled, request) the idle shutdown. It
	// only proceeds once there is genuinely nothing left: no live
	// workspaces AND no create in flight. Deferring via the linger lets a
	// client returning moments later reuse this server instead of racing
	// its shutdown.
	shutdownNow := b.scheduleShutdownIfIdleLocked()
	b.mu.Unlock()

	ws.invokeShutdown()

	if shutdownNow {
		slog.Info("Last workspace removed, shutting down server...")
		b.shutdownFn()
	}
}

// scheduleShutdownIfIdleLocked decides what to do about server shutdown
// after a workspace count or pending-create change. It must be called
// with b.mu held.
//
// It returns true only when the caller should shut the server down
// synchronously (after releasing b.mu) — that is, when the server is idle
// and lingering is disabled (lingerDelay <= 0). When lingering is enabled
// it instead arms a one-shot timer that re-checks idleness after
// lingerDelay and shuts down then, and returns false. When the server is
// not idle (a workspace is live or a create is in flight) it does
// nothing and returns false.
//
// Returning true also latches [Backend.closing], since the caller has to
// release b.mu before it can run shutdownFn.
func (b *Backend) scheduleShutdownIfIdleLocked() (shutdownNow bool) {
	if b.shutdownFn == nil {
		return false
	}
	if b.workspaces.Len() != 0 || b.pending != 0 {
		return false
	}
	if b.lingerDelay <= 0 {
		b.closing = true
		return true
	}
	if b.shutdownTimer == nil {
		b.shutdownTimer = time.AfterFunc(b.lingerDelay, b.maybeShutdown)
	}
	return false
}

// cancelShutdownLocked stops any armed idle-shutdown timer. It must be
// called with b.mu held.
func (b *Backend) cancelShutdownLocked() {
	if b.shutdownTimer != nil {
		b.shutdownTimer.Stop()
		b.shutdownTimer = nil
	}
}

// maybeShutdown is the idle-shutdown timer callback. It shuts the server
// down only if it is still idle when the linger window elapses; any
// create that arrived in the meantime cancelled the timer (or bumped
// pending / the workspace count), so this re-check makes the linger
// race-free. Deciding to exit latches [Backend.closing] under the same
// lock, so a create arriving in the gap before shutdownFn runs is
// refused instead of being initialized on a departing process.
func (b *Backend) maybeShutdown() {
	b.mu.Lock()
	b.shutdownTimer = nil
	idle := b.workspaces.Len() == 0 && b.pending == 0
	if idle {
		b.closing = true
	}
	fn := b.shutdownFn
	b.mu.Unlock()
	if idle && fn != nil {
		slog.Info("Server idle, shutting down...")
		fn()
	}
}

// ShutdownIfIdle shuts the server down only when it is hosting no
// workspaces and has no creates in flight, reporting false when it declined
// because work is live.
//
// This is the only shutdown a client may request. A client asks in order to
// replace a version-mismatched server, and it cannot check idleness itself
// without a second round trip a new session can slip into. Deciding here,
// under the lock creates and teardowns take, closes that window: the answer
// is atomic, and granting it latches the decision so creates arriving
// afterwards are refused.
func (b *Backend) ShutdownIfIdle() bool {
	b.mu.Lock()
	live, pending := b.workspaces.Len(), b.pending
	idle := live == 0 && pending == 0
	if idle {
		b.closing = true
	}
	fn := b.shutdownFn
	b.mu.Unlock()
	if !idle {
		slog.Warn("Refusing shutdown request: server is not idle",
			"workspaces", live, "pending_creates", pending)
		return false
	}
	if fn != nil {
		fn()
	}
	return true
}

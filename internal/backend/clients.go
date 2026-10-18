package backend

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
)

// AttachClient registers a new SSE stream for the given client on the
// workspace. The stream's deferred cleanup must call DetachClient with
// the same arguments to release the claim.
//
// The lookup and the clients-map mutation are performed under
// [Backend.mu] so that AttachClient cannot race with [Backend.teardown]:
// teardown also holds [Backend.mu] while removing the workspace from
// b.workspaces, so once AttachClient observes the workspace and takes
// ws.clientsMu (under b.mu), no concurrent teardown can succeed without
// re-checking the (now non-empty) clients map. Lock order is the
// canonical b.mu -> ws.clientsMu.
func (b *Backend) AttachClient(workspaceID, clientID string) error {
	if _, err := validateClientID(clientID); err != nil {
		return err
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	ws, ok := b.workspaces.Get(workspaceID)
	if !ok {
		return ErrWorkspaceNotFound
	}

	ws.clientsMu.Lock()
	defer ws.clientsMu.Unlock()
	cs, ok := ws.clients[clientID]
	if !ok {
		// Defensive: SSE attach without a prior CreateWorkspace by
		// this client still installs a stream claim so the stream
		// stays alive for its duration.
		ws.clients[clientID] = &clientState{streams: 1}
		return nil
	}
	if cs.holdTimer != nil {
		cs.holdTimer.Stop()
		cs.holdTimer = nil
	}
	cs.streams++
	return nil
}

// DetachClient releases one SSE stream's hold on the workspace. When the
// client has no streams left and no pending creation hold, its claim
// either enters the detach grace — giving a reconnecting client time to
// re-attach — or, if the grace is disabled or the client already released
// its claim, is removed, tearing the workspace down once the refcount
// hits zero.
func (b *Backend) DetachClient(workspaceID, clientID string) {
	ws, ok := b.workspaces.Get(workspaceID)
	if !ok {
		return
	}
	b.detachStream(ws, clientID)
}

// admitLocked reports whether clientID may still take a claim on this
// server. It must be called with b.mu held, which is what makes the
// answer atomic with respect to the shutdown latch and to RetireClient.
func (b *Backend) admitLocked(clientID string) error {
	if b.closing {
		return ErrServerShuttingDown
	}
	if _, ok := b.retired[clientID]; ok {
		return ErrClientRetired
	}
	return nil
}

// RetireClient records that a client has exited and releases every claim it
// holds, across every workspace. It is the authoritative "this client is
// gone" signal, and the reason a client never has to guess whether a create
// whose response it lost left a workspace behind: either the create landed
// first and this call releases its claim, or it arrives later and is
// refused, registering nothing.
//
// Idempotent. Lock order is the canonical b.mu -> ws.clientsMu; teardowns
// for workspaces the client was last on run after b.mu is released and
// re-check under both locks.
func (b *Backend) RetireClient(clientID string) error {
	if _, err := validateClientID(clientID); err != nil {
		return err
	}

	b.mu.Lock()
	if b.retired == nil {
		b.retired = make(map[string]struct{})
	}
	b.retired[clientID] = struct{}{}
	var left, orphaned []*Workspace
	for _, ws := range b.workspaces.Seq2() {
		ws.clientsMu.Lock()
		if cs, ok := ws.clients[clientID]; ok {
			if cs.holdTimer != nil {
				cs.holdTimer.Stop()
			}
			delete(ws.clients, clientID)
			left = append(left, ws)
			if len(ws.clients) == 0 {
				orphaned = append(orphaned, ws)
			}
		}
		ws.clientsMu.Unlock()
	}
	b.mu.Unlock()

	for _, ws := range left {
		b.clientGone(ws, clientID, GoneRetired, slices.Contains(orphaned, ws))
	}
	for _, ws := range orphaned {
		b.teardown(ws)
	}
	return nil
}

// DeleteWorkspace is the public entry point used by the HTTP DELETE
// handler. It releases the named client's creation hold; live streams
// from the same client remain attached and continue holding the
// workspace open until their own deferred DetachClient runs.
func (b *Backend) DeleteWorkspace(id, clientID string) error {
	return b.releaseHold(id, clientID)
}

// releaseHold releases the creation hold for a client, if any. Active
// stream claims are unaffected. Idempotent: returns nil if the
// workspace or the client's hold no longer exist.
func (b *Backend) releaseHold(workspaceID, clientID string) error {
	if _, err := validateClientID(clientID); err != nil {
		return err
	}
	ws, ok := b.workspaces.Get(workspaceID)
	if !ok {
		return nil
	}
	b.releaseHoldLocked(ws, clientID)
	return nil
}

// registerClient installs (idempotently) the given client's claim on
// the workspace and starts a grace timer if the entry is fresh.
//
// A duplicate create from a client whose claim is timer-held (waiting to
// attach, or waiting out the detach grace) re-arms it for a full create
// grace and supersedes any earlier explicit release: the client is coming
// back. The re-arm installs a fresh clientState rather than resetting the
// old timer, so an already-fired timer racing this call fails expireHold's
// identity check instead of killing the new claim.
func (b *Backend) registerClient(ws *Workspace, clientID string) {
	// CreateWorkspace calls this with b.mu held, and no hook may run under
	// it, so the joined hooks run on a goroutine of their own once the
	// claim is in place.
	defer func() {
		go b.runHooks(func(h clientHooks) {
			if h.joined != nil {
				h.joined(ws, clientID)
			}
		})
	}()
	ws.clientsMu.Lock()
	defer ws.clientsMu.Unlock()
	if old, ok := ws.clients[clientID]; ok {
		old.released = false
		if old.holdTimer == nil {
			// Live streams hold the claim; nothing to re-arm.
			return
		}
		old.holdTimer.Stop()
		ws.clients[clientID] = b.newHeldClient(ws, clientID, old.currentSessionID, old.sessionReported, b.createGrace)
		return
	}
	ws.clients[clientID] = b.newHeldClient(ws, clientID, "", false, b.createGrace)
}

// newHeldClient builds a clientState whose claim is held only by a timer
// that releases it after grace. Callers must hold ws.clientsMu.
func (b *Backend) newHeldClient(ws *Workspace, clientID, sessionID string, reported bool, grace time.Duration) *clientState {
	cs := &clientState{currentSessionID: sessionID, sessionReported: reported}
	cs.holdTimer = time.AfterFunc(grace, func() {
		b.expireHold(ws, clientID, cs)
	})
	return cs
}

// expireHold is the body of the grace timer. It runs in its own
// goroutine and races against AttachClient/releaseHold; the timer
// stays valid only while the entry's holdTimer still points at it.
func (b *Backend) expireHold(ws *Workspace, clientID string, timer *clientState) {
	ws.clientsMu.Lock()
	cs, ok := ws.clients[clientID]
	if !ok || cs != timer || cs.holdTimer == nil || cs.streams > 0 {
		ws.clientsMu.Unlock()
		return
	}
	cs.holdTimer = nil
	delete(ws.clients, clientID)
	teardown := len(ws.clients) == 0
	ws.clientsMu.Unlock()
	b.clientGone(ws, clientID, GoneExpired, teardown)
	if teardown {
		b.teardown(ws)
	}
}

func (b *Backend) releaseHoldLocked(ws *Workspace, clientID string) {
	ws.clientsMu.Lock()
	cs, ok := ws.clients[clientID]
	if !ok {
		ws.clientsMu.Unlock()
		return
	}
	if cs.holdTimer != nil {
		cs.holdTimer.Stop()
		cs.holdTimer = nil
	}
	teardown := false
	deleted := false
	if cs.streams == 0 {
		delete(ws.clients, clientID)
		deleted = true
		teardown = len(ws.clients) == 0
	} else {
		// The client gave up its claim while streams are still open, which
		// is what a clean exit looks like. Remember it so the final detach
		// skips the reconnect grace.
		cs.released = true
	}
	ws.clientsMu.Unlock()
	if deleted {
		b.clientGone(ws, clientID, GoneReleased, teardown)
	}
	if teardown {
		b.teardown(ws)
	}
}

func (b *Backend) detachStream(ws *Workspace, clientID string) {
	b.mu.Lock()
	grace := b.detachGrace
	b.mu.Unlock()

	ws.clientsMu.Lock()
	cs, ok := ws.clients[clientID]
	if !ok {
		ws.clientsMu.Unlock()
		return
	}
	if cs.streams > 0 {
		cs.streams--
	}
	teardown := false
	var gone GoneReason // set when the claim ends here
	if cs.streams == 0 && cs.holdTimer == nil {
		if grace > 0 && !cs.released {
			// The stream dropped without the client releasing its claim, so
			// treat it as an interruption rather than an exit: hold the
			// workspace under a timer long enough for the client's
			// reconnect to re-attach (AttachClient stops the timer).
			cs.holdTimer = time.AfterFunc(grace, func() {
				b.expireHold(ws, clientID, cs)
			})
		} else {
			gone = GoneExpired
			if cs.released {
				gone = GoneReleased
			}
			delete(ws.clients, clientID)
			teardown = len(ws.clients) == 0
		}
	}
	ws.clientsMu.Unlock()
	if gone != "" {
		b.clientGone(ws, clientID, gone, teardown)
	}
	if teardown {
		b.teardown(ws)
	}
}

// clientGone runs the gone hooks for clientID, whose claim on ws just
// ended. workspaceEmpty reports that it was the last app. Callers hold no
// lock.
func (b *Backend) clientGone(ws *Workspace, clientID string, reason GoneReason, workspaceEmpty bool) {
	b.runHooks(func(h clientHooks) {
		if h.gone != nil {
			h.gone(ws, clientID, reason, workspaceEmpty)
		}
	})
}

// SetCurrentSession records which session the given client is
// currently viewing within the workspace. Passing an empty sessionID
// clears the client's current-session entry (e.g. the client has
// returned to the landing screen). The recorded session scopes the
// client's SSE stream: permission and question prompts are only
// delivered to clients viewing the session that raised them.
//
// The client must be actually attached — i.e. its [clientState] entry
// must exist and have at least one live stream. A bare creation hold
// (streams == 0) is rejected with [ErrClientNotAttached]. This
// guards against zombie writes from a client that has detached and
// against ghost presence from a hold-only client that never opened an
// SSE stream.
func (b *Backend) SetCurrentSession(ctx context.Context, workspaceID, clientID, sessionID string) error {
	if _, err := validateClientID(clientID); err != nil {
		return err
	}
	ws, ok := b.workspaces.Get(workspaceID)
	if !ok {
		return ErrWorkspaceNotFound
	}
	ws.clientsMu.Lock()
	cs, ok := ws.clients[clientID]
	if !ok || cs.streams == 0 {
		// No entry, or hold-only (no live stream): refuse the
		// write. The presence record this is meant to feed
		// should only reflect clients that can actually observe
		// session events.
		ws.clientsMu.Unlock()
		return ErrClientNotAttached
	}
	cs.currentSessionID = sessionID
	cs.sessionReported = true
	ws.clientsMu.Unlock()

	// A prompt that was raised while no client viewed the session
	// would otherwise stay unanswered until the run is cancelled.
	// Re-publish any pending prompt for the session the client just
	// switched to; the per-client stream filter limits delivery to
	// that session's viewers.
	if sessionID != "" {
		b.republishPendingPrompts(ctx, ws, sessionID)
	}
	b.runHooks(func(h clientHooks) {
		if h.viewChanged != nil {
			h.viewChanged(ws, clientID, sessionID)
		}
	})
	return nil
}

// ClientCurrentSession returns the session the given client is
// currently viewing within the workspace, or "" when the client has
// no session selected (e.g. the landing screen) or is unknown. The
// second return value reports whether the client has ever reported a
// session: a client that never did (e.g. an API client predating
// session reporting) is treated differently from one that explicitly
// cleared its selection.
func (b *Backend) ClientCurrentSession(workspaceID, clientID string) (string, bool) {
	ws, ok := b.workspaces.Get(workspaceID)
	if !ok {
		return "", false
	}
	ws.clientsMu.Lock()
	defer ws.clientsMu.Unlock()
	cs, ok := ws.clients[clientID]
	if !ok {
		return "", false
	}
	return cs.currentSessionID, cs.sessionReported
}

// AttachedClients returns the number of clients currently viewing
// sessionID in the given workspace. Only clients with at least one live
// SSE stream (streams > 0) AND a matching currentSessionID are counted;
// pure creation holds do not contribute. Returns [ErrWorkspaceNotFound]
// if the workspace is unknown.
func (b *Backend) AttachedClients(workspaceID, sessionID string) (int, error) {
	ws, ok := b.workspaces.Get(workspaceID)
	if !ok {
		return 0, ErrWorkspaceNotFound
	}
	return ws.AttachedClientsForSession(sessionID), nil
}

// validateClientID returns the trimmed UUID string or an error if the
// input is empty or not a valid UUID.
func validateClientID(id string) (string, error) {
	if id == "" {
		return "", ErrInvalidClientID
	}
	if _, err := uuid.Parse(id); err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidClientID, err)
	}
	return id, nil
}

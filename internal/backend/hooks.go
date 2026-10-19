package backend

import "github.com/charmbracelet/crush/internal/proto"

// GoneReason says why an app's claim on a workspace ended.
type GoneReason string

const (
	// GoneRetired: the app said goodbye ([Backend.RetireClient]).
	GoneRetired GoneReason = "retired"
	// GoneExpired: its hold ran out, or its last stream dropped while the
	// detach grace is 0.
	GoneExpired GoneReason = "expired"
	// GoneReleased: it released its claim, and no stream of it is left.
	GoneReleased GoneReason = "released"
)

// clientHooks are the calls a build piece gets when an app joins, leaves
// or acts in a workspace (engine-design.md 5.5). Each piece returns its own
// from its <piece>Hooks method; a nil hook is skipped. No hook ever runs
// with b.mu held.
type clientHooks struct {
	// joined runs when clientID registers a claim on ws (a create or a
	// join).
	joined func(ws *Workspace, clientID string)
	// viewChanged runs when clientID reports the chat it shows: sessionID,
	// or "" for none.
	viewChanged func(ws *Workspace, clientID, sessionID string)
	// messageSent runs when a prompt is accepted, before it can reach the
	// scheduler. clientID is "" when the caller named no app.
	messageSent func(ws *Workspace, clientID string, msg proto.AgentMessage)
	// agentInit runs after the agent started; clientID is "" when the
	// caller named no app.
	agentInit func(ws *Workspace, clientID string, interactive bool)
	// gone runs when clientID's claim on ws ended. workspaceEmpty reports
	// that it was the last app, so a teardown is attempted; it can still
	// abort if another app joins first.
	gone func(ws *Workspace, clientID string, reason GoneReason, workspaceEmpty bool)
	// teardown runs when ws is torn down, before it shuts down.
	teardown func(ws *Workspace)
}

// allHooks returns the hooks of every piece: lifecycle, live, tree,
// share and the desktop's phase 5-6 gaps.
func (b *Backend) allHooks() []clientHooks {
	return []clientHooks{b.lifecycleHooks(), b.liveHooks(), b.treeHooks(), b.shareHooks(), b.desktopHooks()}
}

// runHooks calls fn with each piece's hooks, in the order of allHooks. It
// must never be called with b.mu held.
func (b *Backend) runHooks(fn func(clientHooks)) {
	for _, h := range b.allHooks() {
		fn(h)
	}
}

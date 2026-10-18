package backend

// liveState is piece B's state per workspace (live work, ownership and the
// smaller gaps, engine-design.md 5.3, 5.4, 5.5 and 5.9).
type liveState struct{}

// liveHooks are piece B's client hooks.
func (b *Backend) liveHooks() clientHooks { return clientHooks{} }

// MoveToBackground moves the chat's running command to the background and
// reports whether one moved (engine-design.md 5.4). Piece B fills it in;
// until then nothing moves.
func (b *Backend) MoveToBackground(workspaceID, sessionID string) (bool, error) { return false, nil }

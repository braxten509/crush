package dialog

// Locked chats (owner, 2026-11-03): the palette's Lock Chat, Unlock Chat
// and Stop Locking act on the open chat.
type (
	ActionLockChat   struct{}
	ActionUnlockChat struct {
		// Stop unlocks the chat for good: it becomes a normal chat.
		Stop bool
	}
)

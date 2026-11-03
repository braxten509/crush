package backend

import (
	"context"
	"errors"

	"github.com/charmbracelet/crush/internal/lockedchats"
	"github.com/charmbracelet/crush/internal/session"
)

// Locked chats (owner, 2026-11-03): a locked chat's messages live only in
// the Plasma Vault. Locking and unlocking start here and finish in the
// background; apps follow the chat's lock_state in its session events.

// ErrLockingUnavailable: the workspace has no locked-chats service.
var ErrLockingUnavailable = errors.New("locking chats is unavailable here")

// LockSession locks a chat, or locks an unlocked locked chat again.
func (b *Backend) LockSession(ctx context.Context, workspaceID, sessionID string) (session.Session, error) {
	o, err := b.ops(workspaceID)
	if err != nil {
		return session.Session{}, err
	}
	return o.LockSession(ctx, sessionID)
}

// UnlockSession unlocks a locked chat; with stop, for good ("Stop locking
// this chat").
func (b *Backend) UnlockSession(ctx context.Context, workspaceID, sessionID string, stop bool) (session.Session, error) {
	o, err := b.ops(workspaceID)
	if err != nil {
		return session.Session{}, err
	}
	return o.UnlockSession(ctx, sessionID, stop)
}

// LockSession locks a chat. It answers at once with the chat marked
// locking; a busy chat is refused with the gate's reason.
func (o Ops) LockSession(ctx context.Context, sessionID string) (session.Session, error) {
	if o.App == nil || o.App.LockedChats == nil {
		return session.Session{}, ErrLockingUnavailable
	}
	if err := o.App.LockedChats.Lock(ctx, sessionID); err != nil {
		return session.Session{}, err
	}
	return o.App.Sessions.Get(ctx, sessionID)
}

// UnlockSession unlocks a locked chat (KDE asks for the vault password);
// with stop the chat becomes a normal chat for good.
func (o Ops) UnlockSession(ctx context.Context, sessionID string, stop bool) (session.Session, error) {
	if o.App == nil || o.App.LockedChats == nil {
		return session.Session{}, ErrLockingUnavailable
	}
	unlock := o.App.LockedChats.Unlock
	if stop {
		unlock = o.App.LockedChats.StopLocking
	}
	if err := unlock(ctx, sessionID); err != nil {
		return session.Session{}, err
	}
	return o.App.Sessions.Get(ctx, sessionID)
}

// checkReadable refuses to read a hidden chat's messages.
func (o Ops) checkReadable(ctx context.Context, sessionID string) error {
	if o.App == nil || o.App.Sessions == nil {
		return nil
	}
	s, err := o.App.Sessions.Get(ctx, sessionID)
	if err == nil && session.Hidden(s.LockState) {
		return session.ErrChatLocked
	}
	return nil
}

// lockSharing stops a chat's phone share before it is locked: locked
// chats are never shared with the phone, locked or unlocked (owner,
// 2026-11-03).
func (b *Backend) lockSharing(ws *Workspace) {
	if ws.App == nil || ws.App.LockedChats == nil {
		return
	}
	ws.App.LockedChats.BeforeLock = func(_ context.Context, sessionID string) {
		ws.share.mu.Lock()
		sh := ws.share.shares[shareKey("", sessionID)]
		ws.share.mu.Unlock()
		if sh != nil {
			b.endShare(ws, sh, errPhoneShareOff)
		}
	}
}

// errLockedNotShared refuses a phone share of a locked chat.
var errLockedNotShared = errors.New("locked chats are never shared with the phone")

// inheritLock makes a branch of an unlocked locked chat locked too.
func inheritLock(ctx context.Context, ws *Workspace, source, copy string) error {
	if ws.App == nil || ws.App.LockedChats == nil {
		return nil
	}
	return lockedchats.Inherit(ctx, ws.App.Sessions, ws.App.LockedChats.DataDir, source, copy)
}

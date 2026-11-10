package lockedchats

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"path/filepath"
	"slices"

	"github.com/charmbracelet/crush/internal/lock"
	"github.com/charmbracelet/crush/internal/session"
)

// hideOrphans runs before the engine serves requests. Vault access and file
// copying remain in Recover; only the small durable visibility change is here.
func (s *Service) hideOrphans(ctx context.Context) error {
	ids, err := queryStrings(ctx, s.DB, `SELECT id FROM sessions WHERE lock_state = 'unlocked' AND (parent_session_id IS NULL OR parent_session_id = '')`)
	if err != nil {
		return err
	}
	for _, id := range ids {
		release, err := s.chatLock(ctx, id)
		if err != nil {
			return fmt.Errorf("cannot safely check unlocked chat %s: %w", id, err)
		}
		if !liveOwner(s.DataDir, id) && !slices.Contains(ownedHere(s.DataDir), id) {
			_, err = s.DB.ExecContext(ctx, `UPDATE sessions SET lock_state = 'relock', lock_phase = '' WHERE id = ? AND lock_state = 'unlocked'`, id)
		}
		release()
		if err != nil {
			return err
		}
	}
	return nil
}

// Recover finishes or undoes what a crash left (PLAN §4, §9.1, owner
// 2026-11-03). It runs when an engine opens the folder:
//   - locked with wipes pending: the vault copy is complete and the rows
//     are gone, so the wipes finish (no vault needed);
//   - unlocking: the vault copy is still the chat; what was copied back is
//     wiped and the chat is locked again;
//   - a first lock still waiting for the vault: nothing moved, so undo it;
//   - locking, or unlocked with no live Crush owning it: the chat is
//     hidden at once (relock) and put back into the vault, which asks for
//     the password when the vault is closed; cancelled, it stays hidden and
//     Crush tries again on the next Unlock click or start.
func (s *Service) Recover(ctx context.Context) {
	type chat struct{ id, state, phase string }
	rows, err := s.DB.QueryContext(ctx, `SELECT id, lock_state, lock_phase FROM sessions
		WHERE lock_state != '' AND (parent_session_id IS NULL OR parent_session_id = '')`)
	if err != nil {
		slog.Warn("Could not check locked chats", "error", err)
		return
	}
	var chats []chat
	for rows.Next() {
		var c chat
		if err := rows.Scan(&c.id, &c.state, &c.phase); err != nil {
			rows.Close()
			return
		}
		chats = append(chats, c)
	}
	rows.Close()
	var relock []string
	for _, c := range chats {
		if !s.claim(c.id) {
			continue
		}
		if again := s.recoverOne(ctx, c.id, c.state, c.phase); again {
			relock = append(relock, c.id)
			continue
		}
		s.release(c.id)
	}
	for _, id := range relock {
		if err := s.lockNow(ctx, id); err != nil {
			s.notify(id, err)
		}
		s.release(id)
	}
}

// recoverOne repairs one chat's state; true means it must go back into
// the vault.
func (s *Service) recoverOne(ctx context.Context, id, state, phase string) bool {
	if state == session.LockStateUnlocked {
		if liveOwner(s.DataDir, id) || slices.Contains(ownedHere(s.DataDir), id) {
			return false
		}
	}
	// A lock or unlock running in another Crush holds the chat's file
	// lock; that is no crash.
	release, err := lock.TryFile(filepath.Join(s.DataDir, "locks", safeName(id)+".lock"))
	if err != nil {
		if err := ensureDir(filepath.Join(s.DataDir, "locks")); err != nil {
			return false
		}
		if release, err = lock.TryFile(filepath.Join(s.DataDir, "locks", safeName(id)+".lock")); err != nil {
			return false
		}
	}
	defer release()
	// Read again under the lock: it may have finished meanwhile.
	if err := s.DB.QueryRowContext(ctx, `SELECT lock_state, lock_phase FROM sessions WHERE id = ?`, id).Scan(&state, &phase); err != nil {
		if err != sql.ErrNoRows {
			slog.Warn("Could not read a locked chat", "session", id, "error", err)
		}
		return false
	}
	switch state {
	case session.LockStateLocked:
		if phase != "" || s.hasPendingWipes(ctx, id) {
			if err := s.finishLock(ctx, id); err != nil {
				slog.Warn("Could not finish wiping a locked chat's outside copies", "session", id, "error", err)
			}
		}
		return false
	case session.LockStateLocking:
		if phase == phaseOpening {
			if err := s.setState(ctx, id, "", ""); err != nil {
				s.notify(id, err)
			}
			return false
		}
		if phase == "wipe" {
			if err := s.finishLock(ctx, id); err != nil {
				s.notify(id, err)
			}
			return false
		}
		fallthrough
	case session.LockStateUnlocked:
		if err := s.setState(ctx, id, session.LockStateRelock, ""); err != nil {
			return false
		}
		_ = unregister(s.DataDir, id)
		return true
	case session.LockStateUnlocking:
		if err := s.runWipes(ctx, id); err != nil {
			slog.Warn("Could not take back a crashed unlock's copies", "session", id, "error", err)
			return false
		}
		_ = s.setState(ctx, id, session.LockStateLocked, "")
		return false
	case session.LockStateRelock:
		return true
	}
	return false
}

func (s *Service) hasPendingWipes(ctx context.Context, id string) bool {
	var n int
	_ = s.DB.QueryRowContext(ctx, `SELECT count(*) FROM lock_pending_wipes WHERE session_id = ?`, id).Scan(&n)
	return n > 0
}

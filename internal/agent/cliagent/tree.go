package cliagent

import "fmt"

// CloseForTree retires a parked Claude process before changing its context.
// Active turns are excluded by the coordinator's dispatch lock.
func CloseForTree(sessionID string) error {
	return claudeSessions.close(sessionID, func(live *claudeLive) error {
		if live.busy() {
			return fmt.Errorf("wait for Claude's background tasks before switching branches")
		}
		return nil
	})
}

// CloseForLock retires a chat's parked Claude and Codex processes before
// the chat is locked, so their own copies are complete and nothing writes
// to them afterwards (owner, 2026-11-03).
func CloseForLock(sessionID string) error {
	if err := claudeSessions.close(sessionID, func(live *claudeLive) error {
		if live.busy() {
			return fmt.Errorf("wait for Claude's background tasks before locking the chat")
		}
		return nil
	}); err != nil {
		return err
	}
	return codexSessions.close(sessionID, nil)
}

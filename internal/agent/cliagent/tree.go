package cliagent

import "fmt"

// CloseForTree retires a parked Claude process before changing its context.
// Active turns are excluded by the coordinator's dispatch lock.
func CloseForTree(sessionID string) error {
	claudeSessions.mu.Lock()
	live := claudeSessions.m[sessionID]
	if live != nil && live.busy() {
		claudeSessions.mu.Unlock()
		return fmt.Errorf("wait for Claude's background tasks before switching branches")
	}
	if live != nil {
		delete(claudeSessions.m, sessionID)
	}
	claudeSessions.mu.Unlock()
	if live != nil {
		live.timer.Stop()
		close(live.idle)
		<-live.drained
		live.p.finish()
	}
	return nil
}

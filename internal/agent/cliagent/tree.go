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

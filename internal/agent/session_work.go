package agent

import (
	"fmt"

	"github.com/charmbracelet/crush/internal/shell"
)

// SessionBackgroundBlock reports the background work that keeps a tree
// action from running in the chat sessionID, or nil when there is none. It
// counts only that chat's work: Crush's jobs its agent started, including
// those still handing over their end-of-job message; the background
// processes listed for the chat; and unfinished shell jobs whose chat is
// sessionID or unknown. Unknown blocks, so the check fails safe.
func SessionBackgroundBlock(sessionID string) error {
	if sessionShellsPending(sessionID) || len(SessionBackgroundProcesses(sessionID)) > 0 {
		return fmt.Errorf("wait for background commands and their completion messages before switching branches")
	}
	owners := backgroundJobOwners()
	jobs := shell.GetBackgroundShellManager()
	for _, id := range jobs.List() {
		if job, ok := jobs.Get(id); ok && !job.IsDone() {
			if owner, known := owners[id]; !known || owner == sessionID {
				return fmt.Errorf("wait for background commands before switching branches")
			}
		}
	}
	return nil
}

// sessionShellsPending reports Crush's jobs of the chat that still run or
// hand over their end: the hubs' backgroundShells, owned by sessionID.
func sessionShellsPending(sessionID string) bool {
	hubsMu.Lock()
	defer hubsMu.Unlock()
	for _, hub := range hubs {
		pending := false
		hub.mu.Lock()
		for _, job := range hub.backgroundShells {
			if hub.backgroundOwners[job.ID] == sessionID {
				pending = true
				break
			}
		}
		hub.mu.Unlock()
		if pending {
			return true
		}
	}
	return false
}

// backgroundJobOwners maps the shell jobs the hubs watch to the chats whose
// agents started them.
func backgroundJobOwners() map[string]string {
	hubsMu.Lock()
	defer hubsMu.Unlock()
	owners := map[string]string{}
	for _, hub := range hubs {
		hub.mu.Lock()
		for id, sessionID := range hub.backgroundOwners {
			owners[id] = sessionID
		}
		hub.mu.Unlock()
	}
	return owners
}

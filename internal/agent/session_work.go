package agent

import (
	"fmt"

	"github.com/charmbracelet/crush/internal/shell"
)

// SessionBackgroundBlock reports the background work that keeps a tree
// action from running in the chat sessionID, or nil when there is none.
// It still counts the background work of every chat; piece B narrows it to
// the chat (engine-design.md 5.4).
func SessionBackgroundBlock(sessionID string) error {
	if treeBackgroundPending() || len(BackgroundProcesses()) > 0 {
		return fmt.Errorf("wait for background commands and their completion messages before switching branches")
	}
	jobs := shell.GetBackgroundShellManager()
	for _, id := range jobs.List() {
		if job, ok := jobs.Get(id); ok && !job.IsDone() {
			return fmt.Errorf("wait for background commands before switching branches")
		}
	}
	return nil
}

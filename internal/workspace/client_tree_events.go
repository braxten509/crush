package workspace

import (
	tea "charm.land/bubbletea/v2"

	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/pubsub"
)

// translateTreeEvent translates piece C's events (tree actions and branch
// copy, engine-design.md 5.6 and 5.11). It reports false for an event that
// is not C's. Both events have no domain type, so they pass through as
// they are. A tree change from another app leaves the remembered revision
// alone: only a tree read updates it.
func (w *ClientWorkspace) translateTreeEvent(ev any) (tea.Msg, bool) {
	switch ev.(type) {
	case pubsub.Event[proto.TreeChanged], pubsub.Event[proto.ChatLink]:
		return ev, true
	}
	return nil, false
}

package workspace

import (
	tea "charm.land/bubbletea/v2"

	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/pubsub"
)

// translateLifecycleEvent translates piece A's events (engine lifecycle and
// wire, engine-design.md 5.1, 5.2, 5.10 and 5.14). It reports false for an
// event that is not A's.
//
// The terminal does not use prompt_pending (it reads prompts from its own
// chat), so the event is handled and nothing is passed on.
func (w *ClientWorkspace) translateLifecycleEvent(ev any) (tea.Msg, bool) {
	switch ev.(type) {
	case pubsub.Event[proto.PromptPending]:
		return nil, true
	}
	return nil, false
}

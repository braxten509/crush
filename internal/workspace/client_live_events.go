package workspace

import (
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/agent"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/pubsub"
)

// translateLiveEvent translates piece B's events (live work, ownership and
// the smaller gaps, engine-design.md 5.3, 5.4, 5.5 and 5.9). It reports
// false for an event that is not B's.
//
// A helper event becomes the agent's task event, so the terminal's task row
// works in client mode. Live `!` output goes to the command waiting for it,
// not to the UI loop. The other events have no terminal reader yet; they
// are taken and dropped.
func (w *ClientWorkspace) translateLiveEvent(ev any) (tea.Msg, bool) {
	switch e := ev.(type) {
	case pubsub.Event[proto.Helper]:
		return pubsub.Event[agent.Task]{Type: e.Type, Payload: taskFromHelper(e.Payload)}, true
	case pubsub.Event[proto.ShellOutput]:
		w.shellOutputs.deliver(e.Payload)
		return nil, true
	case pubsub.Event[proto.CLILimits], pubsub.Event[proto.MemoryStatus], pubsub.Event[proto.CLIUpdatesInstalled]:
		return nil, true
	default:
		return nil, false
	}
}

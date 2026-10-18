package workspace

import (
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/pubsub"
)

// translateShareEvent translates piece D's events (phone sharing and secure
// entry, engine-design.md 5.7 and 5.8). It reports false for an event that
// is not D's.
//
// The terminal UI does not use them before phase 7 (5.13): it shares and
// enters secrets in-process only. So they are handled with nothing to pass
// on, and no warning is logged.
func (w *ClientWorkspace) translateShareEvent(ev any) (tea.Msg, bool) {
	switch ev.(type) {
	case pubsub.Event[proto.RemoteAction], pubsub.Event[proto.RemoteStatus],
		pubsub.Event[proto.SecureForm], pubsub.Event[proto.SecureFormClosed]:
		return nil, true
	}
	return nil, false
}

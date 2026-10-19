package workspace

import (
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/pubsub"
)

// SecureFormMsg is a secure form the engine sent to this window: a terminal
// that shares the engine shows it (engine-design.md 5.8, 5.13).
type SecureFormMsg struct{ Form proto.SecureForm }

// SecureFormClosedMsg tells the window a secure form closed (submitted,
// cancelled, answered elsewhere or closed).
type SecureFormClosedMsg struct{ proto.SecureFormClosed }

// RemoteStatusMsg is the engine's phone share state for this window.
type RemoteStatusMsg struct{ Status proto.RemoteStatus }

// RemoteActionMsg is a phone request the engine hands to this window.
type RemoteActionMsg struct{ Action proto.RemoteAction }

// translateShareEvent translates piece D's events (phone sharing and secure
// entry, engine-design.md 5.7 and 5.8). It reports false for an event that
// is not D's.
func (w *ClientWorkspace) translateShareEvent(ev any) (tea.Msg, bool) {
	switch e := ev.(type) {
	case pubsub.Event[proto.SecureForm]:
		return SecureFormMsg{Form: e.Payload}, true
	case pubsub.Event[proto.SecureFormClosed]:
		return SecureFormClosedMsg{e.Payload}, true
	case pubsub.Event[proto.RemoteStatus]:
		return RemoteStatusMsg{Status: e.Payload}, true
	case pubsub.Event[proto.RemoteAction]:
		return RemoteActionMsg{Action: e.Payload}, true
	}
	return nil, false
}

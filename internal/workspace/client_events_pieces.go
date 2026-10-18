package workspace

import (
	"fmt"
	"log/slog"

	tea "charm.land/bubbletea/v2"
)

// translatePieceEvent translates the events the build pieces add, each in
// its own client_<piece>_events.go. An event no piece knows is logged and
// dropped, as translateEvent always did.
func (w *ClientWorkspace) translatePieceEvent(ev any) tea.Msg {
	for _, translate := range []func(any) (tea.Msg, bool){
		w.translateLifecycleEvent,
		w.translateLiveEvent,
		w.translateTreeEvent,
		w.translateShareEvent,
	} {
		if msg, ok := translate(ev); ok {
			return msg
		}
	}
	slog.Warn("Unknown event type in translateEvent", "type", fmt.Sprintf("%T", ev))
	return nil
}

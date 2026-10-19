package server

import (
	"fmt"
	"log/slog"

	"github.com/charmbracelet/crush/internal/pubsub"
)

// wrapPieceEvent wraps the fan-in events that the build pieces add, each
// in its own endpoints_<piece>.go. An event no piece knows is logged and
// dropped, as wrapEvent always did.
func wrapPieceEvent(ev any) *pubsub.Payload {
	for _, wrap := range []func(any) (*pubsub.Payload, bool){
		wrapLifecycleEvent,
		wrapLiveEvent,
		wrapTreeEvent,
		wrapShareEvent,
		wrapDesktopEvent,
	} {
		if p, ok := wrap(ev); ok {
			return p
		}
	}
	slog.Warn("Unrecognized event type for SSE wrapping", "type", fmt.Sprintf("%T", ev))
	return nil
}

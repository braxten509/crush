package client

import (
	"encoding/json"
	"maps"

	"github.com/charmbracelet/crush/internal/pubsub"
)

// pieceEventDecoders merges the event decoders the build pieces add, each
// in its own <piece>_events.go.
func pieceEventDecoders() map[pubsub.PayloadType]func(json.RawMessage) any {
	merged := map[pubsub.PayloadType]func(json.RawMessage) any{}
	for _, decoders := range []map[pubsub.PayloadType]func(json.RawMessage) any{
		lifecycleDecoders,
		liveDecoders,
		treeDecoders,
		shareDecoders,
	} {
		maps.Copy(merged, decoders)
	}
	return merged
}

// withPieceDecoders adds the build pieces' decoders to today's decoders.
func withPieceDecoders(decoders map[pubsub.PayloadType]func(json.RawMessage) any) map[pubsub.PayloadType]func(json.RawMessage) any {
	maps.Copy(decoders, pieceEventDecoders())
	return decoders
}

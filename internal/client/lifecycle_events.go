package client

import (
	"encoding/json"

	"github.com/charmbracelet/crush/internal/pubsub"
)

// lifecycleDecoders decodes piece A's events (engine lifecycle and wire,
// engine-design.md 5.1, 5.2, 5.10 and 5.14).
var lifecycleDecoders = map[pubsub.PayloadType]func(json.RawMessage) any{}

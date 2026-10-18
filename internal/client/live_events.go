package client

import (
	"encoding/json"

	"github.com/charmbracelet/crush/internal/pubsub"
)

// liveDecoders decodes piece B's events (live work, ownership and the
// smaller gaps, engine-design.md 5.3, 5.4, 5.5 and 5.9).
var liveDecoders = map[pubsub.PayloadType]func(json.RawMessage) any{}

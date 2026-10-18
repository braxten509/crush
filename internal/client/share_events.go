package client

import (
	"encoding/json"

	"github.com/charmbracelet/crush/internal/pubsub"
)

// shareDecoders decodes piece D's events (phone sharing and secure entry,
// engine-design.md 5.7 and 5.8).
var shareDecoders = map[pubsub.PayloadType]func(json.RawMessage) any{}

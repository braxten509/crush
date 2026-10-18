package client

import (
	"encoding/json"

	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/pubsub"
)

// shareDecoders decodes piece D's events (phone sharing and secure entry,
// engine-design.md 5.7 and 5.8). The engine sends each to one app only.
var shareDecoders = map[pubsub.PayloadType]func(json.RawMessage) any{
	pubsub.PayloadTypeRemoteAction:     decodeEventAs[proto.RemoteAction],
	pubsub.PayloadTypeRemoteStatus:     decodeEventAs[proto.RemoteStatus],
	pubsub.PayloadTypeSecureForm:       decodeEventAs[proto.SecureForm],
	pubsub.PayloadTypeSecureFormClosed: decodeEventAs[proto.SecureFormClosed],
}

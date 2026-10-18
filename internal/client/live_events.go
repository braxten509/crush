package client

import (
	"encoding/json"

	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/pubsub"
)

// liveDecoders decodes piece B's events (live work, ownership and the
// smaller gaps, engine-design.md 5.3, 5.4, 5.5 and 5.9).
var liveDecoders = map[pubsub.PayloadType]func(json.RawMessage) any{
	pubsub.PayloadTypeHelper:              decodeEventAs[proto.Helper],
	pubsub.PayloadTypeShellOutput:         decodeEventAs[proto.ShellOutput],
	pubsub.PayloadTypeLimits:              decodeEventAs[proto.CLILimits],
	pubsub.PayloadTypeMemoryStatus:        decodeEventAs[proto.MemoryStatus],
	pubsub.PayloadTypeCLIUpdatesInstalled: decodeEventAs[proto.CLIUpdatesInstalled],
}

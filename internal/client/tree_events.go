package client

import (
	"encoding/json"

	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/pubsub"
)

// treeDecoders decodes piece C's events (tree actions and branch copy,
// engine-design.md 5.6 and 5.11).
var treeDecoders = map[pubsub.PayloadType]func(json.RawMessage) any{
	pubsub.PayloadTypeTreeChanged: decodeEventAs[proto.TreeChanged],
	pubsub.PayloadTypeChatLink:    decodeEventAs[proto.ChatLink],
}

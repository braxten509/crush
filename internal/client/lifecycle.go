package client

import (
	"context"

	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/rpc"
)

// EngineInfo describes the engine this client talks to (GET /v1/engine).
// It answers while the engine is closing.
func (c *Client) EngineInfo(ctx context.Context) (proto.EngineInfo, error) {
	return newCall(c, rpc.EngineInfo, "get engine info", "engine info").get(ctx)
}

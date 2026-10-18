package server

import (
	"net/http"

	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/rpc"
)

// getEngineInfo describes the engine (GET /v1/engine).
func (c *controllerV1) getEngineInfo(*http.Request, rpc.Empty) (proto.EngineInfo, error) {
	return c.backend.EngineInfo(c.listenAddress()), nil
}

// listenAddress is the address the server listens on, in the form
// --host takes, such as unix:///run/user/1000/crush/engine-….sock, or ""
// when the server was not started on an address (tests).
func (c *controllerV1) listenAddress() string {
	if c.server == nil || c.server.Addr == "" || c.server.network == "" {
		return ""
	}
	return c.server.network + "://" + c.server.Addr
}

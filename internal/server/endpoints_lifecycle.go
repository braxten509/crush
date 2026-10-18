package server

import (
	"errors"
	"net/http"

	"github.com/charmbracelet/crush/internal/apigen"
	"github.com/charmbracelet/crush/internal/backend"
	"github.com/charmbracelet/crush/internal/db"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/charmbracelet/crush/internal/rpc"
)

// Piece A (engine lifecycle and wire, engine-design.md 5.1, 5.2, 5.10 and
// 5.14) binds its routes, wraps its events and maps its errors here.

// lifecycleEndpoints documents and routes piece A's endpoints.
func (c *controllerV1) lifecycleEndpoints() []apigen.Endpoint {
	return []apigen.Endpoint{
		route(c, rpc.EngineInfo, c.getEngineInfo).
			Summary("Get engine info").
			Description("Describes the engine: its protocol, version, process, project data folder, " +
				"address, workspace and apps, and whether it decided to exit. It answers while the " +
				"engine is closing.").
			Tags("system").
			Build(),
	}
}

// wrapLifecycleEvent wraps piece A's fan-in events for SSE. It reports
// false for an event that is not A's. A has none: prompt_pending is
// engine-sent.
func wrapLifecycleEvent(any) (*pubsub.Payload, bool) { return nil, false }

// lifecycleErrors maps piece A's errors to their statuses and codes.
func lifecycleErrors() []apiError {
	is := func(target error) func(error) bool {
		return func(err error) bool { return errors.Is(err, target) }
	}
	return []apiError{
		{Match: is(backend.ErrProtocolMismatch), Status: http.StatusUpgradeRequired, Code: "protocol_mismatch"},
		{Match: is(backend.ErrWrongProject), Status: http.StatusConflict, Code: "wrong_project"},
		{Match: is(backend.ErrChannelOptInMismatch), Status: http.StatusConflict, Code: "channel_mismatch"},
		{Match: is(db.ErrDataDirLocked), Status: http.StatusLocked, Code: "project_locked"},
	}
}

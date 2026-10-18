package rpc

import "github.com/charmbracelet/crush/internal/proto"

// Engine lifecycle endpoints (piece A, engine-design.md 5.2).
var (
	// EngineInfo describes the engine. It answers while the engine is
	// closing, as Version does.
	EngineInfo = Op[Empty, proto.EngineInfo]{Method: get, Path: "/v1/engine"}
)

package rpc

import "github.com/charmbracelet/crush/internal/proto"

// Engine gaps for the desktop app's phases 5 and 6 (desktop design 5-6,
// 9.1, G56). The theme routes are in routes_themes.go and the sign-in
// routes in routes_auth.go.
var (
	// PendingQuestion returns the question form a chat waits on (G-12).
	PendingQuestion = Op[Empty, proto.QuestionRequest]{Method: get, Path: "/v1/workspaces/{id}/questions/pending"}

	// CustomCommands lists the palette's custom commands and skills (G-7).
	CustomCommands = Op[Empty, []proto.CustomCommand]{Method: get, Path: "/v1/workspaces/{id}/commands"}

	// ListServers lists the ports the chats' background work listens on
	// (G-18).
	ListServers = Op[Empty, []proto.ServerPort]{Method: get, Path: "/v1/workspaces/{id}/servers"}

	// RefreshProviders runs `crush update-providers` for each source, then
	// reloads (G-9).
	RefreshProviders = Op[Empty, proto.ProvidersRefresh]{Method: post, Path: "/v1/providers/refresh"}

	// SetCPUAffinity turns the agent CPUs on or off for the engine (G-16).
	SetCPUAffinity = Op[proto.CPUAffinityRequest, proto.CPUAffinity]{Method: post, Path: "/v1/cpu-affinity"}
)

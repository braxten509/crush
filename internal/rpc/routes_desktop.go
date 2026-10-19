package rpc

import "github.com/charmbracelet/crush/internal/proto"

// Engine gaps for the desktop app's phases 5 and 6 (desktop design 5-6,
// 9.1, G56). The theme routes are in routes_themes.go and the sign-in
// routes in routes_auth.go.
var (
	// PendingQuestion returns the question form a chat waits on (G-12).
	PendingQuestion = Op[Empty, proto.QuestionRequest]{Method: get, Path: "/v1/workspaces/{id}/questions/pending"}
)

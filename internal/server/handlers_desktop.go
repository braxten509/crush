package server

import (
	"net/http"

	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/rpc"
)

// Handlers of the desktop app's phase 5-6 routes (G56).

// pendingQuestion returns the question form the chat waits on.
func (c *controllerV1) pendingQuestion(r *http.Request, _ rpc.Empty) (proto.QuestionRequest, error) {
	sid := r.URL.Query().Get("session_id")
	if sid == "" {
		return proto.QuestionRequest{}, badRequest("session_id is required")
	}
	return c.backend.PendingQuestion(r.Context(), r.PathValue("id"), sid)
}

// customCommands lists the palette's custom commands and skills.
func (c *controllerV1) customCommands(r *http.Request, _ rpc.Empty) ([]proto.CustomCommand, error) {
	return c.backend.CustomCommands(r.PathValue("id"))
}

// listServers lists the ports the chats' background work listens on.
func (c *controllerV1) listServers(r *http.Request, _ rpc.Empty) ([]proto.ServerPort, error) {
	return c.backend.ListServers(r.Context(), r.PathValue("id"))
}

// setCPUAffinity turns the agent CPUs on or off.
func (c *controllerV1) setCPUAffinity(_ *http.Request, req proto.CPUAffinityRequest) (proto.CPUAffinity, error) {
	return c.backend.SetCPUAffinity(req.Enabled)
}

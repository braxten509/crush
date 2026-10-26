package server

import (
	"net/http"

	"github.com/charmbracelet/crush/internal/prompthistory"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/rpc"
	"github.com/charmbracelet/crush/internal/skills"
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

// searchSkills searches skills.sh.
func (c *controllerV1) searchSkills(r *http.Request, _ rpc.Empty) ([]skills.DirectorySkill, error) {
	return c.backend.SearchSkills(r.Context(), r.PathValue("id"), r.URL.Query().Get("q"))
}

// refreshProviders refreshes the provider catalogs.
func (c *controllerV1) refreshProviders(r *http.Request, _ rpc.Empty) (proto.ProvidersRefresh, error) {
	return c.backend.RefreshProviders(r.Context()), nil
}

// hyperCredits fetches the Hyper credits balance.
func (c *controllerV1) hyperCredits(r *http.Request, _ rpc.Empty) (proto.HyperCredits, error) {
	return c.backend.HyperCredits(r.Context())
}

// setCPUAffinity turns the agent CPUs on or off.
func (c *controllerV1) setCPUAffinity(_ *http.Request, req proto.CPUAffinityRequest) (proto.CPUAffinity, error) {
	return c.backend.SetCPUAffinity(req.Enabled)
}

// promptHistory lists the prompts sent from every folder and app.
func (c *controllerV1) promptHistory(_ *http.Request, _ rpc.Empty) ([]string, error) {
	return prompthistory.List(), nil
}

// addPromptHistory saves a sent prompt.
func (c *controllerV1) addPromptHistory(_ *http.Request, req proto.PromptHistoryEntry) (rpc.Empty, error) {
	prompthistory.Add(req.Text)
	return rpc.Empty{}, nil
}

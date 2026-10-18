package server

import (
	"net/http"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/rpc"
)

// getAgentInfo returns the agent's model and status.
func (c *controllerV1) getAgentInfo(r *http.Request, _ rpc.Empty) (proto.AgentInfo, error) {
	return c.backend.GetAgentInfo(r.PathValue("id"))
}

// sendMessage accepts a prompt for the agent.
//
// The run's lifetime is detached from the prompting client's HTTP
// request: SendMessage validates and accepts the prompt, dispatches the
// run on a goroutine bound to the workspace context, and returns
// immediately. A client dropping its TCP connection (network blip, TUI
// restart) or another client canceling the session via the explicit
// cancel endpoint can no longer tear down a turn that other subscribed
// clients are still watching. Only the explicit cancel endpoint should
// be able to end a run.
func (c *controllerV1) sendMessage(r *http.Request, msg proto.AgentMessage) (rpc.Empty, error) {
	clientID, err := c.optionalClientID(r)
	if err != nil {
		return rpc.Empty{}, err
	}
	return rpc.Empty{}, c.backend.SendMessage(r.PathValue("id"), clientID, msg)
}

// initAgent initializes the coder agent. The body is optional.
func (c *controllerV1) initAgent(r *http.Request, req proto.AgentInitRequest) (rpc.Empty, error) {
	clientID, err := c.optionalClientID(r)
	if err != nil {
		return rpc.Empty{}, err
	}
	return rpc.Empty{}, c.backend.InitAgent(r.Context(), r.PathValue("id"), clientID, req.Interactive)
}

// updateAgent reloads the agent's model configuration.
func (c *controllerV1) updateAgent(r *http.Request, _ rpc.Empty) (rpc.Empty, error) {
	return rpc.Empty{}, c.backend.UpdateAgent(r.Context(), r.PathValue("id"))
}

// setMainAgent switches the workspace's active agent (e.g. "coder" or
// "plan").
func (c *controllerV1) setMainAgent(r *http.Request, req proto.AgentSetMainRequest) (rpc.Empty, error) {
	return rpc.Empty{}, c.backend.SetMainAgent(r.PathValue("id"), req.AgentID)
}

// getAgentSession returns a session with its busy status.
func (c *controllerV1) getAgentSession(r *http.Request, _ rpc.Empty) (proto.AgentSession, error) {
	return c.backend.GetAgentSession(r.Context(), r.PathValue("id"), r.PathValue("sid"))
}

// cancelAgent cancels a session's running agent turn.
func (c *controllerV1) cancelAgent(r *http.Request, _ rpc.Empty) (rpc.Empty, error) {
	return rpc.Empty{}, c.backend.CancelSession(r.PathValue("id"), r.PathValue("sid"))
}

// getQueuedCount returns the number of queued prompts for a session.
func (c *controllerV1) getQueuedCount(r *http.Request, _ rpc.Empty) (int, error) {
	return c.backend.QueuedPrompts(r.PathValue("id"), r.PathValue("sid"))
}

// clearQueue clears a session's prompt queue.
func (c *controllerV1) clearQueue(r *http.Request, _ rpc.Empty) (rpc.Empty, error) {
	return rpc.Empty{}, c.backend.ClearQueue(r.PathValue("id"), r.PathValue("sid"))
}

// interruptAgent stops the active run and lets the next queued prompt
// run.
func (c *controllerV1) interruptAgent(r *http.Request, _ rpc.Empty) (rpc.Empty, error) {
	return rpc.Empty{}, c.backend.Interrupt(r.PathValue("id"), r.PathValue("sid"))
}

// summarizeSession summarizes a session.
func (c *controllerV1) summarizeSession(r *http.Request, _ rpc.Empty) (rpc.Empty, error) {
	return rpc.Empty{}, c.backend.SummarizeSession(r.Context(), r.PathValue("id"), r.PathValue("sid"))
}

// runShellCommand runs a shell command in the workspace on behalf of the
// session in the path.
func (c *controllerV1) runShellCommand(r *http.Request, req proto.ShellCommandRequest) (proto.ShellCommandResponse, error) {
	req.SessionID = r.PathValue("sid")
	return c.backend.RunShellCommand(r.Context(), r.PathValue("id"), req)
}

// getDefaultSmallModel returns the default small model for a provider.
func (c *controllerV1) getDefaultSmallModel(r *http.Request, _ rpc.Empty) (config.SelectedModel, error) {
	return c.backend.GetDefaultSmallModel(r.PathValue("id"), r.URL.Query().Get("provider_id"))
}

// handlePostWorkspaceAgentSessionPromptRecall withdraws an editable
// queued prompt. With acknowledge=true it instead releases the payload
// retained for a delivered recall and answers with an empty body, so
// the two shapes do not fit one typed handler.
func (c *controllerV1) handlePostWorkspaceAgentSessionPromptRecall(w http.ResponseWriter, r *http.Request) {
	requestID := r.URL.Query().Get("request_id")
	if requestID == "" {
		jsonError(w, http.StatusBadRequest, "request_id is required")
		return
	}
	if r.URL.Query().Get("acknowledge") == "true" {
		c.backend.AcknowledgeRecalledPrompt(r.PathValue("id"), r.PathValue("sid"), requestID)
		w.WriteHeader(http.StatusOK)
		return
	}
	prompt, err := c.backend.RecallQueuedPrompt(r.PathValue("id"), r.PathValue("sid"), requestID)
	if err != nil {
		c.handleError(w, r, err)
		return
	}
	jsonEncode(w, prompt)
}

// handleGetWorkspaceAgentSessionPromptList returns the queued prompts:
// their text, or with details=true their summaries
// ([rpc.QueuedSummaries]).
func (c *controllerV1) handleGetWorkspaceAgentSessionPromptList(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sid := r.PathValue("sid")
	if r.URL.Query().Get("details") == "true" {
		entries, err := c.backend.QueuedPromptSummaries(id, sid)
		if err != nil {
			c.handleError(w, r, err)
			return
		}
		jsonEncode(w, entries)
		return
	}
	prompts, err := c.backend.QueuedPromptsList(id, sid)
	if err != nil {
		c.handleError(w, r, err)
		return
	}
	jsonEncode(w, prompts)
}

// grantPermission grants, persistently grants, or denies a permission
// request.
func (c *controllerV1) grantPermission(r *http.Request, req proto.PermissionGrant) (proto.PermissionGrantResponse, error) {
	resolved, err := c.backend.GrantPermission(r.PathValue("id"), req)
	return proto.PermissionGrantResponse{Resolved: resolved}, err
}

// answerQuestion submits answers for the pending question batch.
func (c *controllerV1) answerQuestion(r *http.Request, req proto.QuestionAnswer) (proto.QuestionAnswerResponse, error) {
	resolved, err := c.backend.AnswerQuestion(r.PathValue("id"), req)
	return proto.QuestionAnswerResponse{Resolved: resolved}, err
}

// cancelQuestion cancels the pending question batch.
func (c *controllerV1) cancelQuestion(r *http.Request, _ rpc.Empty) (proto.QuestionAnswerResponse, error) {
	cancelled, err := c.backend.CancelQuestion(r.PathValue("id"))
	return proto.QuestionAnswerResponse{Resolved: cancelled}, err
}

// setPermissionsSkip sets whether permission prompts are skipped.
func (c *controllerV1) setPermissionsSkip(r *http.Request, req proto.PermissionSkipRequest) (rpc.Empty, error) {
	return rpc.Empty{}, c.backend.SetPermissionsSkip(r.PathValue("id"), req.Skip)
}

// getPermissionsSkip reports whether permission prompts are skipped.
func (c *controllerV1) getPermissionsSkip(r *http.Request, _ rpc.Empty) (proto.PermissionSkipRequest, error) {
	skip, err := c.backend.GetPermissionsSkip(r.PathValue("id"))
	return proto.PermissionSkipRequest{Skip: skip}, err
}

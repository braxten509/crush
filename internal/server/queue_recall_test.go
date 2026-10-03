package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/stretchr/testify/require"
)

type recallCoordinator struct {
	*stubCoordinator
	prompt    *message.QueuedPrompt
	sessionID string
}

func (c *recallCoordinator) RecallQueuedPrompt(sessionID string) *message.QueuedPrompt {
	c.sessionID = sessionID
	prompt := c.prompt
	c.prompt = nil
	return prompt
}

func TestRecallEndpointReturnsAttachmentsAndNullWhenUnavailable(t *testing.T) {
	c, id := buildBusyWorkspace(t, "s1", true)
	ws, err := c.backend.GetWorkspace(id)
	require.NoError(t, err)
	attachment := message.Attachment{FileName: "photo.png", MimeType: "image/png", Content: []byte{1, 2, 3}}
	coord := &recallCoordinator{stubCoordinator: &stubCoordinator{}, prompt: &message.QueuedPrompt{Prompt: "queued", Attachments: []message.Attachment{attachment}}}
	ws.AgentCoordinator = coord
	request := func(query string) *httptest.ResponseRecorder {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/workspaces/"+id+"/agent/sessions/s1/prompts/recall?"+query, nil)
		req.SetPathValue("id", id)
		req.SetPathValue("sid", "s1")
		rsp := httptest.NewRecorder()
		c.handlePostWorkspaceAgentSessionPromptRecall(rsp, req)
		return rsp
	}
	rsp := request("request_id=first")
	require.Equal(t, http.StatusOK, rsp.Code)
	var prompt *proto.AgentMessage
	require.NoError(t, json.Unmarshal(rsp.Body.Bytes(), &prompt))
	require.Equal(t, "s1", coord.sessionID)
	require.Equal(t, "s1", prompt.SessionID)
	require.Equal(t, "queued", prompt.Prompt)
	require.Equal(t, []message.Attachment{attachment}, proto.AttachmentsToMessage(prompt.Attachments))
	// A lost response can be retrieved without claiming the next prompt.
	rsp = request("request_id=first")
	require.Equal(t, http.StatusOK, rsp.Code)
	var retry *proto.AgentMessage
	require.NoError(t, json.Unmarshal(rsp.Body.Bytes(), &retry))
	require.Equal(t, prompt, retry)
	request("request_id=first&acknowledge=true")
	rsp = request("request_id=first")
	require.JSONEq(t, "null", rsp.Body.String())
	rsp = request("request_id=second")
	require.Equal(t, http.StatusOK, rsp.Code)
	require.JSONEq(t, "null", rsp.Body.String())
	require.Equal(t, http.StatusBadRequest, request("").Code)
}

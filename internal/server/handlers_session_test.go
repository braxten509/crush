package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/charmbracelet/crush/internal/backend"
	"github.com/charmbracelet/crush/internal/permission"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/question"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// doAPI sends a JSON request to the harness server and returns the status
// and the body.
func doAPI(t *testing.T, h *e2eHarness, method, path string, body any) (int, []byte) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		require.NoError(t, err)
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(t.Context(), method, h.httpSrv.URL+path, reader)
	require.NoError(t, err)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := h.httpSrv.Client().Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, data
}

// callAPI sends a JSON request and decodes its 200 answer.
func callAPI[T any](t *testing.T, h *e2eHarness, method, path string, body any) T {
	t.Helper()
	status, data := doAPI(t, h, method, path, body)
	require.Equal(t, http.StatusOK, status, string(data))
	var out T
	require.NoError(t, json.Unmarshal(data, &out))
	return out
}

// newRealWorkspace creates a real workspace through the create route and
// returns it with the ID of the app that holds it. The hold is released
// when the test ends.
func newRealWorkspace(t *testing.T, h *e2eHarness) (*backend.Workspace, string) {
	t.Helper()
	h.backend.SetCreateGrace(time.Minute)
	clientID := uuid.NewString()
	created := h.postWorkspace(t, proto.Workspace{Path: t.TempDir(), ClientID: clientID})
	ws, err := h.backend.GetWorkspace(created.ID)
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.backend.DeleteWorkspace(created.ID, clientID) })
	return ws, clientID
}

// sessionsByID lists the workspace's chats through the list route.
func sessionsByID(t *testing.T, h *e2eHarness, workspaceID string) map[string]proto.Session {
	t.Helper()
	list := callAPI[[]proto.Session](t, h, http.MethodGet, "/v1/workspaces/"+workspaceID+"/sessions", nil)
	out := make(map[string]proto.Session, len(list))
	for _, s := range list {
		out[s.ID] = s
	}
	return out
}

func TestSaveSessionKeepsEstimatedUsage(t *testing.T) {
	h := newRealCreateHarness(t)
	ws, _ := newRealWorkspace(t, h)
	ctx := t.Context()

	sess, err := ws.Sessions.Create(ctx, "chat")
	require.NoError(t, err)
	budget := session.ContextBudget{Provider: "anthropic", Model: "claude", Tokens: 150000, Estimated: true}
	sess.ContextBudget = budget
	sess.EstimatedUsage = true
	_, err = ws.Sessions.Save(ctx, sess)
	require.NoError(t, err)

	path := "/v1/workspaces/" + ws.ID + "/sessions/" + sess.ID
	read := callAPI[proto.Session](t, h, http.MethodGet, path, nil)
	require.True(t, read.EstimatedUsage)
	require.Equal(t, proto.ContextBudgetFromDomain(budget), read.ContextBudget)

	// An app renames the chat by sending back the whole record it read.
	read.Title = "renamed"
	saved := callAPI[proto.Session](t, h, http.MethodPut, path, read)
	require.Equal(t, "renamed", saved.Title)
	require.True(t, saved.EstimatedUsage, "the ~ marker survives a save")
	require.Equal(t, proto.ContextBudgetFromDomain(budget), saved.ContextBudget)
	stored, err := ws.Sessions.Get(ctx, sess.ID)
	require.NoError(t, err)
	require.True(t, stored.EstimatedUsage)
	require.Equal(t, budget, stored.ContextBudget)

	// A save stores the budget it is sent.
	read.ContextBudget = &proto.ContextBudget{Provider: "openai", Model: "gpt", Tokens: 200000}
	read.EstimatedUsage = false
	callAPI[proto.Session](t, h, http.MethodPut, path, read)
	stored, err = ws.Sessions.Get(ctx, sess.ID)
	require.NoError(t, err)
	require.False(t, stored.EstimatedUsage)
	require.Equal(t, session.ContextBudget{Provider: "openai", Model: "gpt", Tokens: 200000}, stored.ContextBudget)
}

func TestSessionListPendingPrompt(t *testing.T) {
	h := newRealCreateHarness(t)
	ws, _ := newRealWorkspace(t, h)
	ctx := t.Context()

	chatC, err := ws.Sessions.Create(ctx, "C")
	require.NoError(t, err)
	chatD, err := ws.Sessions.Create(ctx, "D")
	require.NoError(t, err)
	helper, err := ws.Sessions.CreateTaskSession(ctx, uuid.NewString(), chatC.ID, "C's helper")
	require.NoError(t, err)

	askCtx, stopAsking := context.WithCancel(ctx)
	asked := make(chan struct{}, 2)
	t.Cleanup(func() {
		stopAsking()
		<-asked
		<-asked
	})

	// C's helper asks for a permission.
	go func() {
		defer func() { asked <- struct{}{} }()
		_, _ = ws.Permissions.Request(askCtx, permission.CreatePermissionRequest{
			SessionID: helper.ID, ToolCallID: "call-1", ToolName: "bash",
			Action: "execute", Description: "run a command", Path: ws.Path,
		})
	}()
	require.Eventually(t, func() bool {
		_, ok := ws.Permissions.ActiveRequest()
		return ok
	}, 5*time.Second, 10*time.Millisecond)

	chats := sessionsByID(t, h, ws.ID)
	require.Equal(t, "permission", chats[chatC.ID].PendingPrompt, "the helper's prompt counts for its parent chat")
	require.Empty(t, chats[chatD.ID].PendingPrompt)

	// A question in the same chat: the permission wins.
	go func() {
		defer func() { asked <- struct{}{} }()
		_, _ = ws.Questions.Ask(askCtx, question.Request{SessionID: chatC.ID, Questions: []question.Question{{
			ID: "go-on", Type: question.TypeYesNo, Text: "Go on?", Description: "Asks whether to go on.",
		}}})
	}()
	require.Eventually(t, func() bool {
		_, ok := ws.Questions.Pending()
		return ok
	}, 5*time.Second, 10*time.Millisecond)
	require.Equal(t, "permission", sessionsByID(t, h, ws.ID)[chatC.ID].PendingPrompt)

	// Once the permission is granted, the chat waits for its question.
	req, ok := ws.Permissions.ActiveRequest()
	require.True(t, ok)
	require.True(t, ws.Permissions.Grant(req))
	chats = sessionsByID(t, h, ws.ID)
	require.Equal(t, "question", chats[chatC.ID].PendingPrompt)
	require.Empty(t, chats[chatD.ID].PendingPrompt)
	agentSession := callAPI[proto.AgentSession](t, h, http.MethodGet, "/v1/workspaces/"+ws.ID+"/agent/sessions/"+chatC.ID, nil)
	require.Equal(t, "question", agentSession.PendingPrompt)

	require.True(t, ws.Questions.Cancel())
	require.Empty(t, sessionsByID(t, h, ws.ID)[chatC.ID].PendingPrompt)
}

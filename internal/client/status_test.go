package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/skills"
	"github.com/stretchr/testify/require"
)

// clientCalls lists one invocation of every workspace-scoped client
// method that talks plain JSON (the SSE stream has its own test). Each
// entry discards the result and returns only the error.
func clientCalls() map[string]func(context.Context, *Client) error {
	const ws, sid = "ws1", "sess1"
	return map[string]func(context.Context, *Client) error{
		"GetWorkspace": func(ctx context.Context, c *Client) error { _, err := c.GetWorkspace(ctx, ws); return err },
		"DeleteWorkspace": func(ctx context.Context, c *Client) error {
			return c.DeleteWorkspace(ctx, ws)
		},
		"SetCurrentSession": func(ctx context.Context, c *Client) error { return c.SetCurrentSession(ctx, ws, sid) },
		"GetLSPDiagnostics": func(ctx context.Context, c *Client) error {
			_, err := c.GetLSPDiagnostics(ctx, ws, "gopls")
			return err
		},
		"GetLSPs":        func(ctx context.Context, c *Client) error { _, err := c.GetLSPs(ctx, ws); return err },
		"MCPGetStates":   func(ctx context.Context, c *Client) error { _, err := c.MCPGetStates(ctx, ws); return err },
		"MCPPendingAuth": func(ctx context.Context, c *Client) error { _, err := c.MCPPendingAuth(ctx, ws); return err },
		"MCPAuthURL":     func(ctx context.Context, c *Client) error { _, err := c.MCPAuthURL(ctx, ws, "srv"); return err },
		"MCPAuthenticate": func(ctx context.Context, c *Client) error {
			return c.MCPAuthenticate(ctx, ws, "srv")
		},
		"MCPRefreshPrompts":   func(ctx context.Context, c *Client) error { return c.MCPRefreshPrompts(ctx, ws, "srv") },
		"MCPRefreshResources": func(ctx context.Context, c *Client) error { return c.MCPRefreshResources(ctx, ws, "srv") },
		"GetAgentSessionQueuedPrompts": func(ctx context.Context, c *Client) error {
			_, err := c.GetAgentSessionQueuedPrompts(ctx, ws, sid)
			return err
		},
		"ClearAgentSessionQueuedPrompts": func(ctx context.Context, c *Client) error {
			return c.ClearAgentSessionQueuedPrompts(ctx, ws, sid)
		},
		"RecallAgentSessionQueuedPrompt": func(ctx context.Context, c *Client) error {
			_, err := c.RecallAgentSessionQueuedPrompt(ctx, ws, sid)
			return err
		},
		"InterruptAgentSession": func(ctx context.Context, c *Client) error { return c.InterruptAgentSession(ctx, ws, sid) },
		"GetAgentInfo":          func(ctx context.Context, c *Client) error { _, err := c.GetAgentInfo(ctx, ws); return err },
		"UpdateAgent":           func(ctx context.Context, c *Client) error { return c.UpdateAgent(ctx, ws) },
		"SetMainAgent":          func(ctx context.Context, c *Client) error { return c.SetMainAgent(ctx, ws, "coder") },
		"SendMessage": func(ctx context.Context, c *Client) error {
			return c.SendMessage(ctx, ws, sid, "", "", "hi")
		},
		"RunShellCommand": func(ctx context.Context, c *Client) error {
			_, err := c.RunShellCommand(ctx, ws, sid, "ls", 80, false)
			return err
		},
		"GetAgentSessionInfo": func(ctx context.Context, c *Client) error {
			_, err := c.GetAgentSessionInfo(ctx, ws, sid)
			return err
		},
		"AgentSummarizeSession": func(ctx context.Context, c *Client) error { return c.AgentSummarizeSession(ctx, ws, sid) },
		"InitiateAgentProcessing": func(ctx context.Context, c *Client) error {
			return c.InitiateAgentProcessing(ctx, ws, true)
		},
		"ListMessages": func(ctx context.Context, c *Client) error { _, err := c.ListMessages(ctx, ws, sid); return err },
		"GetSession":   func(ctx context.Context, c *Client) error { _, err := c.GetSession(ctx, ws, sid); return err },
		"ListSessionHistoryFiles": func(ctx context.Context, c *Client) error {
			_, err := c.ListSessionHistoryFiles(ctx, ws, sid)
			return err
		},
		"CreateSession": func(ctx context.Context, c *Client) error { _, err := c.CreateSession(ctx, ws, "t"); return err },
		"ListSessions":  func(ctx context.Context, c *Client) error { _, err := c.ListSessions(ctx, ws); return err },
		"GrantPermission": func(ctx context.Context, c *Client) error {
			_, err := c.GrantPermission(ctx, ws, proto.PermissionGrant{Action: proto.PermissionAllow})
			return err
		},
		"AnswerQuestionBatch": func(ctx context.Context, c *Client) error {
			_, err := c.AnswerQuestionBatch(ctx, ws, proto.QuestionAnswer{})
			return err
		},
		"CancelQuestionBatch": func(ctx context.Context, c *Client) error {
			_, err := c.CancelQuestionBatch(ctx, ws)
			return err
		},
		"SetPermissionsSkipRequests": func(ctx context.Context, c *Client) error {
			return c.SetPermissionsSkipRequests(ctx, ws, true)
		},
		"GetPermissionsSkipRequests": func(ctx context.Context, c *Client) error {
			_, err := c.GetPermissionsSkipRequests(ctx, ws)
			return err
		},
		"GetConfig": func(ctx context.Context, c *Client) error { _, err := c.GetConfig(ctx, ws); return err },
		"SaveSession": func(ctx context.Context, c *Client) error {
			_, err := c.SaveSession(ctx, ws, proto.Session{ID: sid})
			return err
		},
		"DeleteSession":    func(ctx context.Context, c *Client) error { return c.DeleteSession(ctx, ws, sid) },
		"ListUserMessages": func(ctx context.Context, c *Client) error { _, err := c.ListUserMessages(ctx, ws, sid); return err },
		"ListAllUserMessages": func(ctx context.Context, c *Client) error {
			_, err := c.ListAllUserMessages(ctx, ws)
			return err
		},
		"CancelAgentSession": func(ctx context.Context, c *Client) error { return c.CancelAgentSession(ctx, ws, sid) },
		"GetAgentSessionQueuedPromptsList": func(ctx context.Context, c *Client) error {
			_, err := c.GetAgentSessionQueuedPromptsList(ctx, ws, sid)
			return err
		},
		"GetAgentSessionQueuedPromptSummaries": func(ctx context.Context, c *Client) error {
			_, err := c.GetAgentSessionQueuedPromptSummaries(ctx, ws, sid)
			return err
		},
		"GetDefaultSmallModel": func(ctx context.Context, c *Client) error {
			_, err := c.GetDefaultSmallModel(ctx, ws, "openai")
			return err
		},
		"FileTrackerRecordRead": func(ctx context.Context, c *Client) error {
			return c.FileTrackerRecordRead(ctx, ws, sid, "a.go")
		},
		"FileTrackerLastReadTime": func(ctx context.Context, c *Client) error {
			_, err := c.FileTrackerLastReadTime(ctx, ws, sid, "a.go")
			return err
		},
		"FileTrackerListReadFiles": func(ctx context.Context, c *Client) error {
			_, err := c.FileTrackerListReadFiles(ctx, ws, sid)
			return err
		},
		"LSPStart":   func(ctx context.Context, c *Client) error { return c.LSPStart(ctx, ws, "a.go") },
		"LSPStopAll": func(ctx context.Context, c *Client) error { return c.LSPStopAll(ctx, ws) },
		"SetConfigField": func(ctx context.Context, c *Client) error {
			return c.SetConfigField(ctx, ws, config.ScopeGlobal, "k", "v")
		},
		"RemoveConfigField": func(ctx context.Context, c *Client) error {
			return c.RemoveConfigField(ctx, ws, config.ScopeGlobal, "k")
		},
		"UpdatePreferredModel": func(ctx context.Context, c *Client) error {
			return c.UpdatePreferredModel(ctx, ws, config.ScopeGlobal, config.SelectedModelTypeLarge, config.SelectedModel{})
		},
		"SetCompactMode": func(ctx context.Context, c *Client) error {
			return c.SetCompactMode(ctx, ws, config.ScopeGlobal, true)
		},
		"SetProviderAPIKey": func(ctx context.Context, c *Client) error {
			return c.SetProviderAPIKey(ctx, ws, config.ScopeGlobal, "openai", "key")
		},
		"ImportCopilot": func(ctx context.Context, c *Client) error { _, _, err := c.ImportCopilot(ctx, ws); return err },
		"RefreshOAuthToken": func(ctx context.Context, c *Client) error {
			return c.RefreshOAuthToken(ctx, ws, config.ScopeGlobal, "openai")
		},
		"ProjectNeedsInitialization": func(ctx context.Context, c *Client) error {
			_, err := c.ProjectNeedsInitialization(ctx, ws)
			return err
		},
		"MarkProjectInitialized": func(ctx context.Context, c *Client) error { return c.MarkProjectInitialized(ctx, ws) },
		"GetInitializePrompt": func(ctx context.Context, c *Client) error {
			_, err := c.GetInitializePrompt(ctx, ws)
			return err
		},
		"ListSkills": func(ctx context.Context, c *Client) error { _, err := c.ListSkills(ctx, ws); return err },
		"ReadSkill":  func(ctx context.Context, c *Client) error { _, err := c.ReadSkill(ctx, ws, "s"); return err },
		"InstalledSkills": func(ctx context.Context, c *Client) error {
			_, err := c.InstalledSkills(ctx, ws)
			return err
		},
		"ManageSkill": func(ctx context.Context, c *Client) error {
			return c.ManageSkill(ctx, ws, "toggle", "s", true, skills.DirectorySkill{})
		},
		"EnableDockerMCP":  func(ctx context.Context, c *Client) error { return c.EnableDockerMCP(ctx, ws) },
		"DisableDockerMCP": func(ctx context.Context, c *Client) error { return c.DisableDockerMCP(ctx, ws) },
		"MCPServersDisabled": func(ctx context.Context, c *Client) error {
			_, err := c.MCPServersDisabled(ctx, ws)
			return err
		},
		"MCPServersEnabled": func(ctx context.Context, c *Client) error {
			_, err := c.MCPServersEnabled(ctx, ws)
			return err
		},
		"SetMCPServerDisabled": func(ctx context.Context, c *Client) error {
			return c.SetMCPServerDisabled(ctx, ws, "srv", true)
		},
		"StartMCPServer": func(ctx context.Context, c *Client) error { return c.StartMCPServer(ctx, ws, "srv") },
		"SetMCPServerConfigDisabled": func(ctx context.Context, c *Client) error {
			return c.SetMCPServerConfigDisabled(ctx, ws, "srv", true)
		},
		"RefreshMCPTools": func(ctx context.Context, c *Client) error { return c.RefreshMCPTools(ctx, ws, "srv") },
		"ReadMCPResource": func(ctx context.Context, c *Client) error {
			_, err := c.ReadMCPResource(ctx, ws, "srv", "file://x")
			return err
		},
		"ListMCPPrompts": func(ctx context.Context, c *Client) error { _, err := c.ListMCPPrompts(ctx, ws); return err },
		"GetMCPPrompt": func(ctx context.Context, c *Client) error {
			_, err := c.GetMCPPrompt(ctx, ws, "srv", "p", nil)
			return err
		},
		"SetKeptSkill": func(ctx context.Context, c *Client) error {
			_, err := c.SetKeptSkill(ctx, ws, sid, proto.KeptSkill{ID: "s"}, true)
			return err
		},
		"GitBranch": func(ctx context.Context, c *Client) error { _, err := c.GitBranch(ctx, ws); return err },
		"LoadMessageReview": func(ctx context.Context, c *Client) error {
			_, err := c.LoadMessageReview(ctx, ws, sid, "m1")
			return err
		},
		"ListSavedSessions": func(ctx context.Context, c *Client) error {
			_, err := c.ListSavedSessions(ctx, ws, "")
			return err
		},
		"ChangeSavedSession": func(ctx context.Context, c *Client) error {
			return c.ChangeSavedSession(ctx, ws, session.Session{ID: sid}, false)
		},
		"DiscardEmptySession": func(ctx context.Context, c *Client) error {
			return c.DiscardEmptySession(ctx, ws, sid)
		},
	}
}

// TestClientCallsMapStatusErrors is the regression for clients that
// hand-rolled their status checks: a 404 from any workspace-scoped
// endpoint must surface as ErrNotFound (so the subscription loop can
// re-register the workspace) and a 503 as ErrServerShuttingDown, and
// both must carry the server's message.
func TestClientCallsMapStatusErrors(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		status int
		want   error
	}{
		{status: http.StatusNotFound, want: ErrNotFound},
		{status: http.StatusServiceUnavailable, want: ErrServerShuttingDown},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(tc.status)
			_ = json.NewEncoder(w).Encode(proto.Error{Message: "workspace not found"})
		}))
		t.Cleanup(srv.Close)
		c := captureClient(t, srv)

		for name, call := range clientCalls() {
			err := call(t.Context(), c)
			require.Error(t, err, "%s: status %d", name, tc.status)
			require.ErrorIs(t, err, tc.want, "%s: status %d: %v", name, tc.status, err)
			if name == "LoadMessageReview" {
				// The review call keeps its user-facing explanation.
				require.Contains(t, err.Error(), "diff details are unavailable")
				continue
			}
			require.Contains(t, err.Error(), "workspace not found", "%s", name)
		}
	}
}

// TestSendMessageStillDefinitiveOnStatusError ensures the status mapping
// keeps the submission error definitive so the UI does not retry.
func TestSendMessageStillDefinitiveOnStatusError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	err := captureClient(t, srv).SendMessage(t.Context(), "ws1", "s", "", "", "hi")
	var definitive *message.DefinitiveSubmissionError
	require.ErrorAs(t, err, &definitive)
	require.ErrorIs(t, err, ErrNotFound)
}

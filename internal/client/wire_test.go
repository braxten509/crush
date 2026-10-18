package client

import (
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// wantRequests pins the method, path, and query (keys, plus values that
// are not per-process tokens) every client call sends. It was written
// against the hand-rolled client that predates the typed rpc
// descriptors, so it guards wire compatibility with existing servers.
var wantRequests = map[string]string{
	"GetWorkspace":                         "GET /v1/workspaces/ws1",
	"DeleteWorkspace":                      "DELETE /v1/workspaces/ws1 client_id",
	"SetCurrentSession":                    "POST /v1/workspaces/ws1/current-session client_id",
	"GetLSPDiagnostics":                    "GET /v1/workspaces/ws1/lsps/gopls/diagnostics",
	"GetLSPs":                              "GET /v1/workspaces/ws1/lsps",
	"MCPGetStates":                         "GET /v1/workspaces/ws1/mcp/states",
	"MCPPendingAuth":                       "GET /v1/workspaces/ws1/mcp/pending-auth",
	"MCPAuthURL":                           "GET /v1/workspaces/ws1/mcp/auth-url name=srv",
	"MCPAuthenticate":                      "POST /v1/workspaces/ws1/mcp/auth",
	"MCPRefreshPrompts":                    "POST /v1/workspaces/ws1/mcp/refresh-prompts",
	"MCPRefreshResources":                  "POST /v1/workspaces/ws1/mcp/refresh-resources",
	"GetAgentSessionQueuedPrompts":         "GET /v1/workspaces/ws1/agent/sessions/sess1/prompts/queued",
	"ClearAgentSessionQueuedPrompts":       "POST /v1/workspaces/ws1/agent/sessions/sess1/prompts/clear",
	"RecallAgentSessionQueuedPrompt":       "POST /v1/workspaces/ws1/agent/sessions/sess1/prompts/recall request_id",
	"InterruptAgentSession":                "POST /v1/workspaces/ws1/agent/sessions/sess1/interrupt",
	"GetAgentInfo":                         "GET /v1/workspaces/ws1/agent",
	"UpdateAgent":                          "POST /v1/workspaces/ws1/agent/update",
	"SetMainAgent":                         "POST /v1/workspaces/ws1/agent/main",
	"SendMessage":                          "POST /v1/workspaces/ws1/agent client_id",
	"RunShellCommand":                      "POST /v1/workspaces/ws1/agent/sessions/sess1/shell",
	"GetAgentSessionInfo":                  "GET /v1/workspaces/ws1/agent/sessions/sess1",
	"AgentSummarizeSession":                "POST /v1/workspaces/ws1/agent/sessions/sess1/summarize",
	"InitiateAgentProcessing":              "POST /v1/workspaces/ws1/agent/init client_id",
	"ListMessages":                         "GET /v1/workspaces/ws1/sessions/sess1/messages",
	"GetSession":                           "GET /v1/workspaces/ws1/sessions/sess1",
	"ListSessionHistoryFiles":              "GET /v1/workspaces/ws1/sessions/sess1/history",
	"CreateSession":                        "POST /v1/workspaces/ws1/sessions",
	"ListSessions":                         "GET /v1/workspaces/ws1/sessions",
	"GrantPermission":                      "POST /v1/workspaces/ws1/permissions/grant",
	"AnswerQuestionBatch":                  "POST /v1/workspaces/ws1/questions/answer",
	"CancelQuestionBatch":                  "POST /v1/workspaces/ws1/questions/cancel",
	"SetPermissionsSkipRequests":           "POST /v1/workspaces/ws1/permissions/skip",
	"GetPermissionsSkipRequests":           "GET /v1/workspaces/ws1/permissions/skip",
	"GetConfig":                            "GET /v1/workspaces/ws1/config",
	"SaveSession":                          "PUT /v1/workspaces/ws1/sessions/sess1",
	"DeleteSession":                        "DELETE /v1/workspaces/ws1/sessions/sess1",
	"ListUserMessages":                     "GET /v1/workspaces/ws1/sessions/sess1/messages/user",
	"ListAllUserMessages":                  "GET /v1/workspaces/ws1/messages/user",
	"CancelAgentSession":                   "POST /v1/workspaces/ws1/agent/sessions/sess1/cancel",
	"GetAgentSessionQueuedPromptsList":     "GET /v1/workspaces/ws1/agent/sessions/sess1/prompts/list",
	"GetAgentSessionQueuedPromptSummaries": "GET /v1/workspaces/ws1/agent/sessions/sess1/prompts/list details=true",
	"GetDefaultSmallModel":                 "GET /v1/workspaces/ws1/agent/default-small-model provider_id=openai",
	"FileTrackerRecordRead":                "POST /v1/workspaces/ws1/filetracker/read",
	"FileTrackerLastReadTime":              "GET /v1/workspaces/ws1/filetracker/lastread path=a.go session_id=sess1",
	"FileTrackerListReadFiles":             "GET /v1/workspaces/ws1/sessions/sess1/filetracker/files",
	"LSPStart":                             "POST /v1/workspaces/ws1/lsps/start",
	"LSPStopAll":                           "POST /v1/workspaces/ws1/lsps/stop",
	"SetConfigField":                       "POST /v1/workspaces/ws1/config/set",
	"RemoveConfigField":                    "POST /v1/workspaces/ws1/config/remove",
	"UpdatePreferredModel":                 "POST /v1/workspaces/ws1/config/model",
	"SetCompactMode":                       "POST /v1/workspaces/ws1/config/compact",
	"SetProviderAPIKey":                    "POST /v1/workspaces/ws1/config/provider-key",
	"ImportCopilot":                        "POST /v1/workspaces/ws1/config/import-copilot",
	"RefreshOAuthToken":                    "POST /v1/workspaces/ws1/config/refresh-oauth",
	"ProjectNeedsInitialization":           "GET /v1/workspaces/ws1/project/needs-init",
	"MarkProjectInitialized":               "POST /v1/workspaces/ws1/project/init",
	"GetInitializePrompt":                  "GET /v1/workspaces/ws1/project/init-prompt",
	"ListSkills":                           "GET /v1/workspaces/ws1/skills",
	"ReadSkill":                            "POST /v1/workspaces/ws1/skills/read",
	"InstalledSkills":                      "GET /v1/workspaces/ws1/skills/installed",
	"ManageSkill":                          "POST /v1/workspaces/ws1/skills/manage",
	"EnableDockerMCP":                      "POST /v1/workspaces/ws1/mcp/docker/enable",
	"DisableDockerMCP":                     "POST /v1/workspaces/ws1/mcp/docker/disable",
	"MCPServersDisabled":                   "GET /v1/workspaces/ws1/mcp/disabled",
	"MCPServersEnabled":                    "GET /v1/workspaces/ws1/mcp/enabled",
	"SetMCPServerDisabled":                 "POST /v1/workspaces/ws1/mcp/disabled",
	"StartMCPServer":                       "POST /v1/workspaces/ws1/mcp/start",
	"SetMCPServerConfigDisabled":           "POST /v1/workspaces/ws1/mcp/config-disabled",
	"RefreshMCPTools":                      "POST /v1/workspaces/ws1/mcp/refresh-tools",
	"ReadMCPResource":                      "POST /v1/workspaces/ws1/mcp/read-resource",
	"ListMCPPrompts":                       "GET /v1/workspaces/ws1/mcp/prompts",
	"GetMCPPrompt":                         "POST /v1/workspaces/ws1/mcp/get-prompt",
	"SetKeptSkill":                         "POST /v1/workspaces/ws1/sessions/sess1/kept-skills",
	"GitBranch":                            "GET /v1/workspaces/ws1/git/branch",
	"LoadMessageReview":                    "GET /v1/workspaces/ws1/sessions/sess1/messages/m1/review",
	"ListSavedSessions":                    "GET /v1/workspaces/ws1/saved-sessions selected=",
	"ChangeSavedSession":                   "POST /v1/workspaces/ws1/saved-sessions",
	"DiscardEmptySession":                  "POST /v1/workspaces/ws1/saved-sessions",
}

// tokenParams are query parameters whose values are per-process tokens;
// only their presence is pinned.
var tokenParams = map[string]bool{"client_id": true, "request_id": true}

func describeRequest(r *http.Request) string {
	parts := []string{r.Method + " " + r.URL.Path}
	q := r.URL.Query()
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if tokenParams[k] {
			parts = append(parts, k)
			continue
		}
		parts = append(parts, k+"="+q.Get(k))
	}
	return strings.Join(parts, " ")
}

// TestClientCallsKeepTheirWireShape checks every client call against the
// requests the hand-rolled client sent, and that JSON bodies are labelled
// as such.
func TestClientCallsKeepTheirWireShape(t *testing.T) {
	t.Parallel()

	calls := clientCalls()
	require.Len(t, wantRequests, len(calls), "every client call must have a pinned request")

	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := make(chan string, 2)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.ContentLength > 0 {
					require.Equal(t, "application/json", r.Header.Get("Content-Type"), name)
				}
				select {
				case got <- describeRequest(r):
				default:
				}
				w.WriteHeader(http.StatusOK)
			}))
			t.Cleanup(srv.Close)

			require.NoError(t, call(t.Context(), captureClient(t, srv)))
			require.Equal(t, wantRequests[name], <-got)
		})
	}
}

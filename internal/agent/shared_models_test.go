package agent

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/charmbracelet/crush/internal/agent/prompt"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/stretchr/testify/require"
)

// A subagent outlives the credential it was built with: a refresh renews the
// credential and revokes the old one while the subagent keeps working. The
// refresh rebuilds the models every agent shares, so the subagent's retry
// must go out with the renewed credential rather than the rejected one.
func TestSubAgentRetriesWithRenewedCredential(t *testing.T) {
	const chunk = `{"id":"c","object":"chat.completion.chunk","created":0,"model":"mock-model","choices":[{"index":0,"delta":%s,"finish_reason":%s}]}`

	var (
		mu   sync.Mutex
		seen []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		// Title generation runs in its own goroutine and can land after the
		// subagent's retry, so only the subagent's requests are recorded.
		if !strings.Contains(string(body), "Generate a concise title") {
			mu.Lock()
			seen = append(seen, auth)
			mu.Unlock()
		}
		if auth != "Bearer second" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"message":"token revoked","type":"authentication_error"}}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: "+chunk+"\n\n", `{"role":"assistant","content":"done"}`, "null")
		fmt.Fprintf(w, "data: "+chunk+"\n\n", `{}`, `"stop"`)
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)

	env := testEnv(t)
	crushJSON := fmt.Sprintf(`{
  "options": {"disable_default_providers": true, "disable_provider_auto_update": true},
  "providers": {"mock": {"id": "mock", "name": "Mock", "type": "openai-compat",
    "base_url": %q, "api_key": "first",
    "models": [{"id": "mock-model", "name": "Mock", "context_window": 8192, "default_max_tokens": 128}]}},
  "models": {"large": {"provider": "mock", "model": "mock-model"},
             "small": {"provider": "mock", "model": "mock-model"}}
}`, srv.URL)
	require.NoError(t, os.WriteFile(filepath.Join(env.workingDir, "crush.json"), []byte(crushJSON), 0o644))
	cfg, err := config.Init(t.Context(), env.workingDir, "", false)
	require.NoError(t, err)
	cfg.SetupAgents()

	coord := &coordinator{
		cfg:         cfg,
		sessions:    env.sessions,
		messages:    env.messages,
		permissions: env.permissions,
		history:     env.history,
		filetracker: *env.filetracker,
	}
	p, err := taskPrompt(prompt.WithWorkingDir(env.workingDir))
	require.NoError(t, err)
	sub, err := coord.buildAgent(t.Context(), &coord.readyWg, p, cfg.Config().Agents[config.AgentTask], true)
	require.NoError(t, err)
	require.NoError(t, coord.readyWg.Wait())

	// The credential is renewed from a key template, the simplest refresh
	// that needs no token endpoint.
	t.Setenv("CRUSH_SHARED_MODELS_TEST_KEY", "second")
	pc, ok := cfg.Config().Providers.Get("mock")
	require.True(t, ok)
	pc.APIKeyTemplate = "$CRUSH_SHARED_MODELS_TEST_KEY"
	cfg.Config().Providers.Set("mock", pc)

	parent, err := env.sessions.Create(t.Context(), "parent")
	require.NoError(t, err)
	resp, err := coord.runSubAgent(t.Context(), subAgentParams{
		Agent:          sub,
		SessionID:      parent.ID,
		AgentMessageID: "message",
		ToolCallID:     "call",
		Prompt:         "go",
		SessionTitle:   "sub",
	})
	require.NoError(t, err)
	require.False(t, resp.IsError, resp.Content)
	require.Equal(t, "done", strings.TrimSpace(resp.Content))

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, "Bearer second", seen[len(seen)-1])
}

func TestTurnKeepsItsModelAcrossARebuild(t *testing.T) {
	t.Parallel()

	model := func(provider, id string) Model {
		return Model{
			Model:    &finishStreamModel{text: provider + "/" + id},
			ModelCfg: config.SelectedModel{Provider: provider, Model: id},
		}
	}
	started := model("openai", "gpt")
	// The fork keeps a build per chat choice (chatModels), not one shared
	// model set: a turn looks up the build of the model it started on.
	builds := 0
	a := &sessionAgent{chatModels: chatModelCache{byKey: map[string]Model{}, build: func(_ context.Context, sel config.SelectedModel) (Model, error) {
		builds++
		return model(sel.Provider, sel.Model), nil
	}}}

	rebuilt := a.currentBuildOf(started)
	require.NotSame(t, started.Model, rebuilt, "a refreshed build of the same model is used")
	require.Same(t, rebuilt, a.currentBuildOf(started), "the kept build is reused")
	require.Equal(t, 1, builds)
}

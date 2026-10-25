package agent

import (
	"context"
	"sync"
	"testing"

	"charm.land/catwalk/pkg/catwalk"
	"charm.land/fantasy"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/stretchr/testify/require"
)

// A chat runs on the model saved with it, so a model picked in another chat
// never reaches it (owner, 2026-10-23).
func TestTurnUsesTheChatsOwnModel(t *testing.T) {
	env := testEnv(t)
	workspace := &finishStreamModel{text: "workspace model"}
	own := &finishStreamModel{text: "the chat's model"}
	agent := testSessionAgent(env, workspace, &finishStreamModel{text: "title"}, "system").(*sessionAgent)

	var built []config.SelectedModel
	agent.chatModels.build = func(_ context.Context, selection config.SelectedModel) (Model, error) {
		built = append(built, selection)
		return Model{
			Model:      own,
			CatwalkCfg: catwalk.Model{ContextWindow: 200_000, DefaultMaxTokens: 10_000},
			ModelCfg:   selection,
		}, nil
	}

	chat, err := env.sessions.Create(t.Context(), "own model")
	require.NoError(t, err)
	chat, err = env.sessions.SetModel(t.Context(), chat.ID, session.ModelChoice(`{"provider":"claude-code","model":"sonnet","reasoning_effort":"high"}`))
	require.NoError(t, err)
	require.Contains(t, string(chat.Model), "claude-code")

	_, err = agent.Run(t.Context(), SessionAgentCall{SessionID: chat.ID, Prompt: "hello", NonInteractive: true})
	require.NoError(t, err)
	require.Equal(t, []config.SelectedModel{{Provider: "claude-code", Model: "sonnet", ReasoningEffort: "high"}}, built, "the chat's own model is built")
	require.Contains(t, assistantText(t, env, chat.ID), "the chat's model", "the reply came from the chat's own model")

	// A second turn reuses the model it built, rather than building it again.
	_, err = agent.Run(t.Context(), SessionAgentCall{SessionID: chat.ID, Prompt: "again", NonInteractive: true})
	require.NoError(t, err)
	require.Len(t, built, 1, "a chat's model is built once")

	// Another account (or FAST, Ultracode) on the same model runs elsewhere,
	// so it is built again rather than reusing the first account's model.
	_, err = env.sessions.SetModel(t.Context(), chat.ID, session.ModelChoice(`{"provider":"claude-code","model":"sonnet","reasoning_effort":"high","account":"claude-2"}`))
	require.NoError(t, err)
	_, err = agent.Run(t.Context(), SessionAgentCall{SessionID: chat.ID, Prompt: "other account", NonInteractive: true})
	require.NoError(t, err)
	require.Len(t, built, 2, "a new account builds the model again")
	require.Equal(t, "claude-2", built[1].Account)

	// A chat without a model of its own keeps the workspace's.
	other, err := env.sessions.Create(t.Context(), "workspace model")
	require.NoError(t, err)
	require.Empty(t, other.Model)
	_, err = agent.Run(t.Context(), SessionAgentCall{SessionID: other.ID, Prompt: "hello", NonInteractive: true})
	require.NoError(t, err)
	require.Len(t, built, 2, "the workspace's model is not rebuilt")
}

// SetModel stores the chat's model and clearing it puts the chat back on the
// workspace's.
func TestSessionModelIsStoredPerChat(t *testing.T) {
	env := testEnv(t)
	first, err := env.sessions.Create(t.Context(), "first")
	require.NoError(t, err)
	second, err := env.sessions.Create(t.Context(), "second")
	require.NoError(t, err)

	choice := session.ModelChoice(`{"provider":"p","model":"m","reasoning_effort":"max","max_tokens":4096}`)
	_, err = env.sessions.SetModel(t.Context(), first.ID, choice)
	require.NoError(t, err)

	read, err := env.sessions.Get(t.Context(), first.ID)
	require.NoError(t, err)
	require.JSONEq(t, string(choice), string(read.Model))

	untouched, err := env.sessions.Get(t.Context(), second.ID)
	require.NoError(t, err)
	require.Empty(t, untouched.Model, "another chat keeps the workspace's model")

	// Saving the chat (usage, title, todos) keeps its model.
	read.Title = "renamed"
	_, err = env.sessions.Save(t.Context(), read)
	require.NoError(t, err)
	read, err = env.sessions.Get(t.Context(), first.ID)
	require.NoError(t, err)
	require.Contains(t, string(read.Model), `"m"`, "a save keeps the chat's model")

	_, err = env.sessions.SetModel(t.Context(), first.ID, nil)
	require.NoError(t, err)
	read, err = env.sessions.Get(t.Context(), first.ID)
	require.NoError(t, err)
	require.Empty(t, read.Model, "a cleared model puts the chat back on the workspace's")
}

// assistantText is what the chat's newest assistant reply says.
func assistantText(t *testing.T, env fakeEnv, sessionID string) string {
	t.Helper()
	messages, err := env.messages.List(t.Context(), sessionID)
	require.NoError(t, err)
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != message.Assistant {
			continue
		}
		return messages[i].Content().Text
	}
	return ""
}

// recordingStreamModel answers like finishStreamModel and keeps every call
// it was sent.
type recordingStreamModel struct {
	finishStreamModel
	mu    sync.Mutex
	calls []fantasy.Call
}

func (m *recordingStreamModel) Stream(ctx context.Context, call fantasy.Call) (fantasy.StreamResponse, error) {
	m.mu.Lock()
	m.calls = append(m.calls, call)
	m.mu.Unlock()
	return m.finishStreamModel.Stream(ctx, call)
}

// chatModelCoordinator is a coordinator whose one chat runs on its own
// model, on the provider "mock2" (default reply length 4321), while the
// workspace's model is "mock" (128). It returns the chat's model, the chat
// and how many times the chat's model was built.
func chatModelCoordinator(t *testing.T) (*coordinator, *recordingStreamModel, string, *int) {
	t.Helper()
	c := newGateTestCoordinator(t, false)
	_ = c.readyWg.Wait()
	c.cfg.Config().Providers.Set("mock2", config.ProviderConfig{
		ID: "mock2", Name: "Mock 2", Type: "openai", BaseURL: "http://127.0.0.1:9/v1", APIKey: "key-2",
		Models: []catwalk.Model{{ID: "m2", Name: "Mock 2", ContextWindow: 200_000, DefaultMaxTokens: 4321}},
	})
	own := &recordingStreamModel{finishStreamModel: finishStreamModel{text: "the chat's model"}}
	built := 0
	c.currentAgent().(*sessionAgent).chatModels.build = func(_ context.Context, selection config.SelectedModel) (Model, error) {
		built++
		return Model{Model: own, CatwalkCfg: *c.cfg.Config().GetModel(selection.Provider, selection.Model), ModelCfg: selection}, nil
	}
	chat, err := c.sessions.Create(t.Context(), "own model")
	require.NoError(t, err)
	_, err = c.sessions.SetModel(t.Context(), chat.ID, session.ModelChoice(`{"provider":"mock2","model":"m2","temperature":0.3}`))
	require.NoError(t, err)
	return c, own, chat.ID, &built
}

// The request's settings come from the model the chat runs on, not from
// the workspace's (owner, 2026-10-24: no shared model anywhere).
func TestTurnSettingsComeFromTheChatsModel(t *testing.T) {
	c, own, chat, _ := chatModelCoordinator(t)
	_, err := c.Run(t.Context(), chat, "hello")
	require.NoError(t, err)
	require.NotEmpty(t, own.calls, "the chat's own model answered")
	call := own.calls[0]
	require.NotNil(t, call.MaxOutputTokens)
	require.Equal(t, int64(4321), *call.MaxOutputTokens, "the chat's model's reply length")
	require.NotNil(t, call.Temperature)
	require.InDelta(t, 0.3, *call.Temperature, 1e-9, "the chat's model's temperature")
}

// A refreshed sign-in builds the chat's model again, so its next request
// carries the new key instead of the one it was first built with.
func TestARefreshedSignInRebuildsTheChatsModel(t *testing.T) {
	c, _, chat, built := chatModelCoordinator(t)
	_, err := c.Run(t.Context(), chat, "hello")
	require.NoError(t, err)
	_, err = c.Run(t.Context(), chat, "again")
	require.NoError(t, err)
	require.Equal(t, 1, *built, "a chat's model is built once")

	providerCfg, _ := c.cfg.Config().Providers.Get("mock2")
	providerCfg.APIKeyTemplate = "key-3"
	require.NoError(t, c.refreshApiKeyTemplate(t.Context(), providerCfg))
	_, err = c.Run(t.Context(), chat, "after the refresh")
	require.NoError(t, err)
	require.Equal(t, 2, *built, "the refresh built the chat's model again")
}

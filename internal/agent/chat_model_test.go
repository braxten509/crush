package agent

import (
	"context"
	"testing"

	"charm.land/catwalk/pkg/catwalk"

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

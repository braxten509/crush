package agent

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"charm.land/catwalk/v3/pkg/catwalk"
	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/stretchr/testify/require"
)

// titleFailModel is a model used only for titles: it counts every request
// and fails it.
type titleFailModel struct {
	finishStreamModel
	titleCalls atomic.Int32
}

func (m *titleFailModel) Generate(context.Context, fantasy.Call) (*fantasy.Response, error) {
	m.titleCalls.Add(1)
	return nil, errors.New("title generation failed")
}

func (m *titleFailModel) Stream(context.Context, fantasy.Call) (fantasy.StreamResponse, error) {
	m.titleCalls.Add(1)
	return nil, errors.New("title generation failed")
}

// titleCountModel wraps a streaming model and counts title generation calls.
type titleCountModel struct {
	finishStreamModel
	titleCalls atomic.Int32
}

func (m *titleCountModel) Stream(ctx context.Context, call fantasy.Call) (fantasy.StreamResponse, error) {
	if isTitleCall(call) {
		m.titleCalls.Add(1)
	}
	return m.finishStreamModel.Stream(ctx, call)
}

func isTitleCall(call fantasy.Call) bool {
	for _, msg := range call.Prompt {
		for _, part := range msg.Content {
			if tp, ok := part.(fantasy.TextPart); ok && len(tp.Text) >= 24 && tp.Text[:24] == "Generate a concise title" {
				return true
			}
		}
	}
	return false
}

// The first send in a branch keeps the title the branch was made with, even
// when titling would rename any other chat.
func TestBranchTitleNeverAutoReplaced(t *testing.T) {
	env := testEnv(t)
	model := &titleCountModel{finishStreamModel: finishStreamModel{text: "Sure."}}
	sa := testSessionAgent(env, model, model, "system").(*sessionAgent)
	ctx := t.Context()

	// A chat that opens with the assistant's greeting holds no user text,
	// so its first send starts titling.
	greeted := func(title string) (string, message.Message) {
		chat, err := env.sessions.Create(ctx, title)
		require.NoError(t, err)
		greeting, err := env.messages.Create(ctx, chat.ID, message.CreateMessageParams{
			Role:  message.Assistant,
			Parts: []message.ContentPart{message.TextContent{Text: "Hello! Where shall we go?"}, message.Finish{Reason: message.FinishReasonEndTurn}},
		})
		require.NoError(t, err)
		return chat.ID, greeting
	}
	titleOf := func(id string) string {
		chat, err := env.sessions.Get(ctx, id)
		if err != nil {
			return ""
		}
		return chat.Title
	}

	// The control: the same chat, not a branch, gets renamed.
	control, _ := greeted("Control")
	_, err := sa.Run(ctx, SessionAgentCall{SessionID: control, Prompt: "Plan a trip", NonInteractive: true})
	require.NoError(t, err)
	require.Eventually(t, func() bool { return titleOf(control) != "Control" }, 5*time.Second, 10*time.Millisecond)
	tried := model.titleCalls.Load()
	require.Positive(t, tried, "the control asked for a title")

	source, greeting := greeted("Source")
	branch, err := env.messages.(message.BranchService).Branch(ctx, message.BranchRequest{
		SessionID: source, MessageID: greeting.ID,
		HighlightText: "Hello", HighlightStart: 0, HighlightEnd: 5,
		Title: "Hello, explained",
	})
	require.NoError(t, err)

	_, err = sa.Run(ctx, SessionAgentCall{SessionID: branch.SessionID, Prompt: "Plan a trip", NonInteractive: true})
	require.NoError(t, err)
	// Titling runs in the background, as it did for the control.
	require.Never(t, func() bool { return titleOf(branch.SessionID) != "Hello, explained" }, 300*time.Millisecond, 10*time.Millisecond)

	// Run to its end, titling leaves the branch alone and asks no model.
	sa.GenerateTitle(ctx, branch.SessionID, "Plan a trip")
	require.Equal(t, "Hello, explained", titleOf(branch.SessionID))
	require.Equal(t, tried, model.titleCalls.Load())
}

// A title is asked of the chat's own model directly ("self"), never a
// separate helper model or any other model.
func TestTitleUsesTheChatModel(t *testing.T) {
	env := testEnv(t)
	ctx := t.Context()
	other := &titleFailModel{}
	helper := &titleFailModel{}
	sa := testSessionAgent(env, other, helper, "system").(*sessionAgent)
	own := &finishStreamModel{text: "Trip to Lisbon"}
	sa.chatModels.build = func(_ context.Context, selection config.SelectedModel) (Model, error) {
		return Model{Model: own, CatwalkCfg: catwalk.Model{ContextWindow: 200_000, DefaultMaxTokens: 10_000}, ModelCfg: selection}, nil
	}
	chat, err := env.sessions.Create(ctx, "New Session")
	require.NoError(t, err)
	_, err = env.sessions.SetModel(ctx, chat.ID, session.ModelChoice(`{"provider":"p","model":"m"}`))
	require.NoError(t, err)

	sa.GenerateTitle(ctx, chat.ID, "Plan a trip to Lisbon")
	chat, err = env.sessions.Get(ctx, chat.ID)
	require.NoError(t, err)
	require.Equal(t, "Trip to Lisbon", chat.Title, "the chat's own model titled it directly")
	require.Zero(t, helper.titleCalls.Load(), "the helper is not asked when the chat has its own model")
	require.Zero(t, other.titleCalls.Load(), "no other model is asked")
}

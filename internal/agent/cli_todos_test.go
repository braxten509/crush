package agent

import (
	"context"
	"encoding/json"
	"testing"

	"charm.land/fantasy"
	"github.com/stretchr/testify/require"

	"github.com/charmbracelet/crush/internal/agent/cliagent"
	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/session"
)

func todoSteps(t *testing.T) (*cliSteps, *sessionAgent, session.Session, *[]fantasy.ToolResultContent) {
	t.Helper()
	env := testEnv(t)
	sa := NewSessionAgent(SessionAgentOptions{Sessions: env.sessions, Messages: env.messages}).(*sessionAgent)
	sess, err := env.sessions.Create(t.Context(), "todos")
	require.NoError(t, err)
	var results []fantasy.ToolResultContent
	s := &cliSteps{running: toolRunningForSteer(), ctx: t.Context(), a: sa, sessionID: sess.ID, sc: fantasy.AgentStreamCall{
		PrepareStep: func(ctx context.Context, _ fantasy.PrepareStepFunctionOptions) (context.Context, fantasy.PrepareStepResult, error) {
			return ctx, fantasy.PrepareStepResult{}, nil
		},
		OnToolCall:   func(fantasy.ToolCallContent) error { return nil },
		OnToolResult: func(r fantasy.ToolResultContent) error { results = append(results, r); return nil },
	}}
	require.NoError(t, s.begin())
	return s, sa, sess, &results
}

func savedTodos(t *testing.T, sa *sessionAgent, id string) []session.Todo {
	t.Helper()
	sess, err := sa.sessions.Get(t.Context(), id)
	require.NoError(t, err)
	return sess.Todos
}

func TestCLIToDoToolCallFillsTheChatList(t *testing.T) {
	t.Parallel()
	s, sa, sess, results := todoSteps(t)
	input := `{"todos":[{"content":"Write the fix","status":"completed","active_form":"Writing the fix"},{"content":"Run the tests","status":"in_progress","active_form":"Running the tests"}]}`
	require.NoError(t, s.handle(cliagent.Event{Type: cliagent.EventToolCall, ID: "t1", Name: tools.TodosToolName, Input: input}))
	require.Equal(t, []session.Todo{
		{Content: "Write the fix", Status: session.TodoStatusCompleted, ActiveForm: "Writing the fix"},
		{Content: "Run the tests", Status: session.TodoStatusInProgress, ActiveForm: "Running the tests"},
	}, savedTodos(t, sa, sess.ID))

	require.NoError(t, s.handle(cliagent.Event{Type: cliagent.EventToolResult, ID: "t1", Name: tools.TodosToolName, Output: "ok"}))
	require.Len(t, *results, 1)
	var meta tools.TodosResponseMetadata
	require.NoError(t, json.Unmarshal([]byte((*results)[0].ClientMetadata), &meta))
	require.True(t, meta.IsNew, "the chat shows the list the way the built-in tool does")
	require.Equal(t, 2, meta.Total)
	require.Equal(t, 1, meta.Completed)
}

func TestCLIPlanUpdateFillsTheChatList(t *testing.T) {
	t.Parallel()
	s, sa, sess, results := todoSteps(t)
	require.NoError(t, s.handle(cliagent.Event{Type: cliagent.EventTodos, Input: `{"todos":[{"content":"Read the code","status":"in_progress","active_form":""}]}`}))
	require.Equal(t, []session.Todo{{Content: "Read the code", Status: session.TodoStatusInProgress}}, savedTodos(t, sa, sess.ID))
	require.Empty(t, *results, "a plan update adds nothing to the chat itself")

	require.NoError(t, s.handle(cliagent.Event{Type: cliagent.EventTodos, Input: `{"todos":[{"content":"Read the code","status":"weird"}]}`}))
	require.Len(t, savedTodos(t, sa, sess.ID), 1, "a list Crush can't read is left alone")
}

func TestModelSwitchHandsOverTheToDoList(t *testing.T) {
	t.Parallel()
	env := testEnv(t)
	sa := NewSessionAgent(SessionAgentOptions{Sessions: env.sessions, Messages: env.messages}).(*sessionAgent)
	sess, err := env.sessions.Create(t.Context(), "switch")
	require.NoError(t, err)
	sess.Todos = []session.Todo{{Content: "Fix the hang", Status: session.TodoStatusCompleted}, {Content: "Check the phone", Status: session.TodoStatusPending}}
	_, err = env.sessions.Save(t.Context(), sess)
	require.NoError(t, err)
	_, err = env.messages.Create(t.Context(), sess.ID, message.CreateMessageParams{Role: message.Assistant, Provider: "anthropic", Model: "claude-opus-5-5"})
	require.NoError(t, err)
	// Notices without a model don't count as the last model's reply.
	_, err = env.messages.Create(t.Context(), sess.ID, message.CreateMessageParams{Role: message.Assistant})
	require.NoError(t, err)

	require.Empty(t, sa.handOverTodos(t.Context(), sess.ID, "anthropic", "claude-opus-5-5"), "the same model keeps its list")
	require.Len(t, savedTodos(t, sa, sess.ID), 2)

	note := sa.handOverTodos(t.Context(), sess.ID, "claude-code", "opus")
	require.Contains(t, note, "- [completed] Fix the hang")
	require.Contains(t, note, "- [pending] Check the phone")
	require.Empty(t, savedTodos(t, sa, sess.ID), "a different model starts with an empty list")
	require.Empty(t, sa.handOverTodos(t.Context(), sess.ID, "claude-code", "opus"), "nothing left to hand over")
}

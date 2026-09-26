package agent

import (
	"context"
	"testing"

	"charm.land/fantasy"

	"github.com/charmbracelet/crush/internal/agent/cliagent"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/stretchr/testify/require"
)

func TestCLIHandoff(t *testing.T) {
	msg := func(id string, role message.MessageRole, text string) message.Message {
		return message.Message{ID: id, Role: role, Provider: "codex-cli", Model: "gpt", Parts: []message.ContentPart{message.TextContent{Text: text}}}
	}
	history := []message.Message{
		msg("1", message.User, "remember PINEAPPLE"),
		msg("2", message.Assistant, "ok"),
		msg("3", message.User, "what now"),
		msg("4", message.Assistant, "codex answer"),
	}

	// New session: everything is handed over, nothing resumed.
	prompt, resume := cliHandoff(history, cliagent.Link{}, "next")
	require.Empty(t, resume)
	require.Contains(t, prompt, "PINEAPPLE")
	require.Contains(t, prompt, "[assistant: codex-cli/gpt]\ncodex answer")
	require.True(t, len(prompt) > len("next") && prompt[len(prompt)-4:] == "next")

	// Resumed session that saw up to message 2: only 3 and 4 are new.
	prompt, resume = cliHandoff(history, cliagent.Link{Native: "n1", Through: "2"}, "next")
	require.Equal(t, "n1", resume)
	require.NotContains(t, prompt, "PINEAPPLE")
	require.Contains(t, prompt, "codex answer")

	// Resumed session that is up to date: just the prompt.
	prompt, resume = cliHandoff(history, cliagent.Link{Native: "n1", Through: "4"}, "next")
	require.Equal(t, "n1", resume)
	require.Equal(t, "next", prompt)

	// Its last-seen message was summarized away: start over with everything.
	_, resume = cliHandoff(history, cliagent.Link{Native: "n1", Through: "gone"}, "next")
	require.Empty(t, resume)
}

func TestCLISteer(t *testing.T) {
	t.Parallel()
	env := testEnv(t)
	sa := NewSessionAgent(SessionAgentOptions{Sessions: env.sessions, Messages: env.messages}).(*sessionAgent)
	sess, err := env.sessions.Create(t.Context(), "session")
	require.NoError(t, err)

	var steps int
	s := &cliSteps{ctx: t.Context(), a: sa, sessionID: sess.ID, sc: fantasy.AgentStreamCall{
		PrepareStep: func(ctx context.Context, _ fantasy.PrepareStepFunctionOptions) (context.Context, fantasy.PrepareStepResult, error) {
			return ctx, fantasy.PrepareStepResult{}, nil
		},
		OnStepFinish: func(fantasy.StepResult) error { steps++; return nil },
	}}
	require.NoError(t, s.begin())

	// Queued prompts go to the CLI together, and show up in the chat as
	// their own step boundary once it takes them in.
	sa.enqueueCall(SessionAgentCall{SessionID: sess.ID, Prompt: "one"})
	sa.enqueueCall(SessionAgentCall{SessionID: sess.ID, Prompt: "two"})
	require.Equal(t, "one\n\ntwo", s.steer())
	require.Zero(t, sa.QueuedPrompts(sess.ID))
	require.Empty(t, s.steer())
	require.NoError(t, s.handle(cliagent.Event{Type: cliagent.EventUserMessage, Text: "one\n\ntwo"}))
	require.Equal(t, 1, steps)
	msgs, err := env.messages.List(t.Context(), sess.ID)
	require.NoError(t, err)
	require.Len(t, msgs, 2)
	require.Equal(t, "two", msgs[1].Content().String())

	// A prompt the CLI never took in goes back to the front of the queue.
	sa.enqueueCall(SessionAgentCall{SessionID: sess.ID, Prompt: "three"})
	require.Equal(t, "three", s.steer())
	sa.enqueueCall(SessionAgentCall{SessionID: sess.ID, Prompt: "four"})
	s.returnUnsteered()
	require.Equal(t, []string{"three", "four"}, sa.QueuedPromptsList(sess.ID))

	// Unless the turn was canceled.
	require.Equal(t, "three\n\nfour", s.steer())
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	s.ctx = ctx
	s.returnUnsteered()
	require.Zero(t, sa.QueuedPrompts(sess.ID))
}

package agent

import (
	"context"
	"testing"
	"time"

	"charm.land/catwalk/pkg/catwalk"
	"charm.land/fantasy"
	"github.com/stretchr/testify/require"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/message"
)

// What the user types in a sub-agent's own chat goes to the sub-agent, not
// the main agent: into its running turn, then as a follow-up once it is
// done. Either way its result goes to the parent.
func TestChatMessageGoesToItsSubAgent(t *testing.T) {
	parentPrompts := make(chan string, 4)
	c := sessionRunTestCoordinator(t, func(_ context.Context, call SessionAgentCall) (*fantasy.AgentResult, error) {
		parentPrompts <- call.Prompt
		return fakeSessionReply(call, "received"), nil
	})
	h := resumeTestHub(t, c)
	c.tasks = h
	c.cfg.Config().Providers.Set("claude", config.ProviderConfig{ID: "claude", Type: config.TypeClaudeCode, Models: []catwalk.Model{{ID: "opus"}}})
	parent, child := helperChats(t, c)
	reported := func(want string) {
		t.Helper()
		select {
		case prompt := <-parentPrompts:
			require.Contains(t, prompt, want)
		case <-time.After(5 * time.Second):
			t.Fatalf("the parent was not handed %q", want)
		}
	}

	// Running: the typed message joins its turn, and the task's result is
	// the reply that came after it.
	typed := make(chan SessionAgentCall, 1)
	sub := newMockAgent("claude", 4096, func(ctx context.Context, call SessionAgentCall) (*fantasy.AgentResult, error) {
		if call.Prompt == "work" {
			<-typed
			return agentResultWithText("first report"), nil
		}
		typed <- call
		return agentResultWithText("report after the user's message"), nil
	})
	m := Model{CatwalkCfg: catwalk.Model{DefaultMaxTokens: 4096}}
	_, err := h.start(t.Context(), func() {}, &Task{SessionID: parent.ID, ChildID: child.ID, Name: "worker", CLI: "claude", Model: "opus", Status: TaskRunning, Started: time.Now()}, sub, m, config.ProviderConfig{ID: "claude", Type: config.TypeClaudeCode}, "work")
	require.NoError(t, err)
	require.True(t, c.IsSessionBusy(child.ID), "its chat is busy while it runs")
	_, err = c.Run(message.WithSubmissionID(t.Context(), "typed-1"), child.ID, "steer")
	require.NoError(t, err)
	reported("report after the user's message")

	// Done: the typed message is a follow-up, sent as typed.
	require.Eventually(t, func() bool { return !c.IsSessionBusy(child.ID) }, 5*time.Second, 10*time.Millisecond)
	followUp := make(chan SessionAgentCall, 1)
	h.subAgentHook = func(config.ProviderConfig, catwalk.Model) SessionAgent {
		return newMockAgent("claude", 4096, func(_ context.Context, call SessionAgentCall) (*fantasy.AgentResult, error) {
			followUp <- call
			return agentResultWithText("follow-up report"), nil
		})
	}
	_, err = c.Run(message.WithSubmissionID(t.Context(), "typed-2"), child.ID, "more please")
	require.NoError(t, err)
	select {
	case call := <-followUp:
		require.Equal(t, child.ID, call.SessionID)
		require.Equal(t, "more please", call.Prompt)
		require.Equal(t, "typed-2", call.SubmissionID, "the app sees its prompt end")
	case <-time.After(5 * time.Second):
		t.Fatal("the finished sub-agent got no follow-up")
	}
	reported("follow-up report")
}

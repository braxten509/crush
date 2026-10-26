package agent

import (
	"context"
	"encoding/json"
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

// The agent that started a sub-agent can message it while it works, as the
// user can in its chat (owner, 2026-10-25): `crush spawn --continue` puts
// the message in line, and --interrupt also cuts its current turn short.
// Either way the sub-agent's one result reaches the parent.
func TestAgentMessagesRunningSubAgent(t *testing.T) {
	for _, interrupt := range []bool{false, true} {
		t.Run(map[bool]string{false: "in line", true: "interrupt"}[interrupt], func(t *testing.T) {
			parentPrompts := make(chan string, 4)
			c := sessionRunTestCoordinator(t, func(_ context.Context, call SessionAgentCall) (*fantasy.AgentResult, error) {
				parentPrompts <- call.Prompt
				return fakeSessionReply(call, "received"), nil
			})
			h := resumeTestHub(t, c)
			c.tasks = h
			parent, child := helperChats(t, c)

			// The message is held in line, as a busy chat holds it, and its
			// turn comes once the current one ends.
			cut, arrived, messaged := make(chan struct{}), make(chan struct{}), make(chan SessionAgentCall, 1)
			var sub *mockSessionAgent
			sub = newMockAgent("claude", 4096, func(ctx context.Context, call SessionAgentCall) (*fantasy.AgentResult, error) {
				if call.Prompt == "work" {
					if interrupt {
						<-cut
						return nil, context.Canceled
					}
					<-arrived
					return agentResultWithText("first report"), nil
				}
				messaged <- call
				close(arrived)
				go func() {
					if interrupt {
						<-cut
					}
					call.report.fill(agentResultWithText("report after the message"), nil)
				}()
				return nil, nil
			})
			sub.interrupt = func(sessionID string) {
				require.Equal(t, child.ID, sessionID)
				close(cut)
			}
			m := Model{CatwalkCfg: catwalk.Model{DefaultMaxTokens: 4096}}
			task, err := h.start(t.Context(), func() {}, &Task{SessionID: parent.ID, ChildID: child.ID, Name: "worker", CLI: "claude", Model: "opus", Status: TaskRunning, Started: time.Now()}, sub, m, config.ProviderConfig{ID: "claude", Type: config.TypeClaudeCode}, "work")
			require.NoError(t, err)

			req, err := json.Marshal(TaskRequest{Session: parent.ID, Continue: task.ID, Prompt: "also check the error paths", Interrupt: interrupt})
			require.NoError(t, err)
			reply := h.handle(req)
			require.Empty(t, reply.Error)
			require.Contains(t, reply.Message, task.ID)
			select {
			case call := <-messaged:
				require.Equal(t, child.ID, call.SessionID)
				require.Contains(t, call.Prompt, "also check the error paths")
				require.Contains(t, call.Prompt, "agent that started you")
			case <-time.After(5 * time.Second):
				t.Fatal("the running sub-agent never got the message")
			}
			select {
			case prompt := <-parentPrompts:
				require.Contains(t, prompt, "report after the message")
			case <-time.After(5 * time.Second):
				t.Fatal("the parent never got the sub-agent's result")
			}
		})
	}
}

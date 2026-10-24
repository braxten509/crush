package agent

import (
	"context"
	"testing"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/agent/cliagent"
	"github.com/stretchr/testify/require"
)

type interruptProbeTool struct{ calls int }

func (*interruptProbeTool) Info() fantasy.ToolInfo { return fantasy.ToolInfo{Name: "work"} }
func (p *interruptProbeTool) Run(ctx context.Context, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
	if err := ctx.Err(); err != nil {
		return fantasy.ToolResponse{}, err
	}
	p.calls++
	return fantasy.NewTextResponse("worked"), nil
}
func (*interruptProbeTool) ProviderOptions() fantasy.ProviderOptions   { return nil }
func (*interruptProbeTool) SetProviderOptions(fantasy.ProviderOptions) {}

func newToolInterruptTurn(t *testing.T, limit int, subAgent, needsUpdate bool) (*turn, *sessionAgent, context.Context, func()) {
	t.Helper()
	env := testEnv(t)
	agent := NewSessionAgent(SessionAgentOptions{Sessions: env.sessions, Messages: env.messages}).(*sessionAgent)
	agent.isSubAgent = subAgent
	sess, err := env.sessions.Create(t.Context(), "tool interrupt")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	current := &turn{
		a:             agent,
		call:          SessionAgentCall{SessionID: sess.ID, ProgressUpdateRequired: needsUpdate},
		genCtx:        ctx,
		cancel:        cancel,
		toolCallLimit: limit,
	}
	return current, agent, ctx, cancel
}

func requireHiddenUpdateQueued(t *testing.T, agent *sessionAgent, sessionID string) {
	t.Helper()
	queued, ok := agent.messageQueue.Get(sessionID)
	require.True(t, ok)
	require.Len(t, queued, 1)
	require.True(t, queued[0].HiddenUserMessage)
	require.True(t, queued[0].ProgressUpdateRequired)
	require.Contains(t, queued[0].Prompt, "update the user")
	require.Empty(t, agent.QueuedPromptsList(sessionID), "the silent nudge must stay out of the visible queue")
}

func TestToolCallInterruptCancelsThresholdCallAndQueuesHiddenUpdate(t *testing.T) {
	current, agent, ctx, cancel := newToolInterruptTurn(t, 3, false, false)
	defer cancel()
	tool := &interruptProbeTool{}
	wrapped := progressInterruptTool{AgentTool: tool, turn: current}

	for range 2 {
		_, err := wrapped.Run(ctx, fantasy.ToolCall{Name: "work"})
		require.NoError(t, err)
	}
	_, err := wrapped.Run(ctx, fantasy.ToolCall{Name: "work"})
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 2, tool.calls, "the threshold call must not keep running")
	require.ErrorIs(t, ctx.Err(), context.Canceled)
	requireHiddenUpdateQueued(t, agent, current.call.SessionID)
}

func TestProgressNudgePrecedesAlreadyQueuedUserPrompts(t *testing.T) {
	current, agent, ctx, cancel := newToolInterruptTurn(t, 1, false, false)
	defer cancel()
	agent.enqueueCall(SessionAgentCall{SessionID: current.call.SessionID, Prompt: "USER_WAITING", SubmissionID: "user-1"})
	wrapped := progressInterruptTool{AgentTool: &interruptProbeTool{}, turn: current}

	_, err := wrapped.Run(ctx, fantasy.ToolCall{Name: "work"})
	require.ErrorIs(t, err, context.Canceled)
	queued, ok := agent.messageQueue.Get(current.call.SessionID)
	require.True(t, ok)
	require.Len(t, queued, 2)
	require.True(t, queued[0].ProgressUpdateRequired, "the user update must run before the queued request")
	require.Equal(t, "USER_WAITING", queued[1].Prompt)
	require.Equal(t, []string{"USER_WAITING"}, agent.QueuedPromptsList(current.call.SessionID))
}

func TestVisibleTextResetsToolCallCount(t *testing.T) {
	current, _, ctx, cancel := newToolInterruptTurn(t, 3, false, false)
	defer cancel()
	tool := &interruptProbeTool{}
	wrapped := progressInterruptTool{AgentTool: tool, turn: current}

	for range 2 {
		_, err := wrapped.Run(ctx, fantasy.ToolCall{Name: "work"})
		require.NoError(t, err)
	}
	current.noteVisibleAssistantText()
	for range 2 {
		_, err := wrapped.Run(ctx, fantasy.ToolCall{Name: "work"})
		require.NoError(t, err)
	}
	require.Equal(t, 4, tool.calls)
	require.NoError(t, ctx.Err())
}

func TestHiddenNudgeBlocksToolsUntilVisibleUpdate(t *testing.T) {
	current, agent, ctx, cancel := newToolInterruptTurn(t, 21, false, true)
	defer cancel()
	tool := &interruptProbeTool{}
	wrapped := progressInterruptTool{AgentTool: tool, turn: current}

	_, err := wrapped.Run(ctx, fantasy.ToolCall{Name: "work"})
	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, tool.calls)
	requireHiddenUpdateQueued(t, agent, current.call.SessionID)

	current, _, ctx, cancel = newToolInterruptTurn(t, 21, false, true)
	defer cancel()
	current.noteVisibleAssistantText()
	tool = &interruptProbeTool{}
	wrapped = progressInterruptTool{AgentTool: tool, turn: current}
	_, err = wrapped.Run(ctx, fantasy.ToolCall{Name: "work"})
	require.NoError(t, err)
	require.Equal(t, 1, tool.calls)
}

func TestSubAgentToolCallsAreNotInterrupted(t *testing.T) {
	current, agent, ctx, cancel := newToolInterruptTurn(t, 1, true, false)
	defer cancel()
	tool := &interruptProbeTool{}
	wrapped := progressInterruptTool{AgentTool: tool, turn: current}

	_, err := wrapped.Run(ctx, fantasy.ToolCall{Name: "work"})
	require.NoError(t, err)
	require.Equal(t, 1, tool.calls)
	require.NoError(t, ctx.Err())
	require.Zero(t, agent.QueuedPrompts(current.call.SessionID))
}

func TestManualCancellationDoesNotQueueProgressNudge(t *testing.T) {
	current, agent, ctx, cancel := newToolInterruptTurn(t, 1, false, false)
	cancel()
	tool := &interruptProbeTool{}
	wrapped := progressInterruptTool{AgentTool: tool, turn: current}

	_, err := wrapped.Run(ctx, fantasy.ToolCall{Name: "work"})
	require.ErrorIs(t, err, context.Canceled)
	queued, ok := agent.messageQueue.Get(current.call.SessionID)
	require.False(t, ok)
	require.Empty(t, queued, "a user cancel must not start a progress-update turn")
}

func TestCLIToolStartUsesTheSameInterruptionRule(t *testing.T) {
	current, agent, ctx, cancel := newToolInterruptTurn(t, 1, false, false)
	defer cancel()
	steps := &cliSteps{
		ctx:             ctx,
		sessionID:       current.call.SessionID,
		onToolInterrupt: current.interruptOnToolCall,
		sc: fantasy.AgentStreamCall{
			OnToolInputStart: func(string, string) error { return nil },
		},
	}

	err := steps.handleToolStart(cliagent.Event{ID: "call-1", Name: "bash"})
	require.ErrorIs(t, err, context.Canceled)
	requireHiddenUpdateQueued(t, agent, current.call.SessionID)
}

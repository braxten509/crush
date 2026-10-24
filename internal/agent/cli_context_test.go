package agent

import (
	"testing"

	"charm.land/fantasy"
	"github.com/stretchr/testify/require"

	"github.com/charmbracelet/crush/internal/agent/cliagent"
	"github.com/charmbracelet/crush/internal/config"
)

// TestCLIContextUsedReplacesTheTurnTotal: OpenCode's turn result adds up
// every step, which can pass the context window after one busy turn. Its
// usage_update carries the context actually in use; that is what the session
// must show.
func TestCLIContextUsedReplacesTheTurnTotal(t *testing.T) {
	env := testEnv(t)
	sa := NewSessionAgent(SessionAgentOptions{Sessions: env.sessions, Messages: env.messages}).(*sessionAgent)
	sess, err := env.sessions.Create(t.Context(), "context")
	require.NoError(t, err)
	s := &cliSteps{ctx: t.Context(), a: sa, sessionID: sess.ID, m: &cliagent.Model{Kind: config.TypeOpenCodeCLI, ID: "opencode-go/deepseek-v4.1-flash"}}

	require.NoError(t, s.handleContextBudget(cliagent.Event{Type: cliagent.EventContextBudget, ContextLimit: 968000, ContextUsed: 123456}))
	got, err := sa.sessions.Get(t.Context(), sess.ID)
	require.NoError(t, err)
	require.EqualValues(t, 123456, got.PromptTokens)
	require.Zero(t, got.CompletionTokens)
	require.EqualValues(t, 968000, got.ContextBudget.Tokens)

	// A later step's count replaces it; the budget is unchanged.
	require.NoError(t, s.handleContextBudget(cliagent.Event{Type: cliagent.EventContextBudget, ContextLimit: 968000, ContextUsed: 130000}))
	got, err = sa.sessions.Get(t.Context(), sess.ID)
	require.NoError(t, err)
	require.EqualValues(t, 130000, got.PromptTokens)

	// The step's own save uses the context in use, not the turn's total.
	var saved fantasy.Usage
	s.open = true
	s.usage = fantasy.Usage{InputTokens: 1_132_994, OutputTokens: 50}
	s.sc = fantasy.AgentStreamCall{OnStepFinish: func(step fantasy.StepResult) error {
		saved = step.Usage
		return nil
	}}
	require.NoError(t, s.finish(fantasy.FinishReasonStop))
	require.EqualValues(t, 130000, saved.InputTokens)
	require.Zero(t, saved.OutputTokens)
}

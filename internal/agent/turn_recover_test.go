package agent

import (
	"context"
	"os"
	"strings"
	"testing"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/stretchr/testify/require"
)

func TestRunTurnPanicBecomesChatError(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	c := sessionRunTestCoordinator(t, func(context.Context, SessionAgentCall) (*fantasy.AgentResult, error) {
		panic("boom")
	})
	session, err := c.sessions.Create(t.Context(), "panic test")
	require.NoError(t, err)

	_, err = c.Run(t.Context(), session.ID, "hello")
	require.ErrorContains(t, err, "internal error")
	require.ErrorContains(t, err, "boom")

	msgs, err := c.messages.List(t.Context(), session.ID)
	require.NoError(t, err)
	var finish *message.Finish
	for i := range msgs {
		if f := msgs[i].FinishPart(); f != nil && f.Reason == message.FinishReasonError {
			finish = f
		}
	}
	require.NotNil(t, finish, "the chat should show the internal error")
	require.Equal(t, "Crush hit an internal error", finish.Message)
	_, path, ok := strings.Cut(finish.Details, "Details saved to ")
	require.True(t, ok, finish.Details)
	report, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(report), "boom")
}

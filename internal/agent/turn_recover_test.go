package agent

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/stretchr/testify/require"
)

func TestRunTurnPanicBecomesChatError(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
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

	// The crash report is saved under the state folder.
	var reported bool
	require.NoError(t, filepath.WalkDir(state, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		reported = reported || strings.Contains(string(data), "boom")
		return nil
	}))
	require.True(t, reported, "a saved report names the panic")
}

package backend

import (
	"context"
	"testing"

	"github.com/charmbracelet/crush/internal/agent"
	"github.com/charmbracelet/crush/internal/app"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// titleCoordinator records GenerateTitle calls.
type titleCoordinator struct {
	agent.Coordinator
	sessionID, prompt string
}

func (c *titleCoordinator) GenerateTitle(_ context.Context, sessionID, prompt string) {
	c.sessionID, c.prompt = sessionID, prompt
}

// TestRunShellCommand_TitlesFirstMessage covers the drift where only the
// in-process workspace titled a session whose first message was a shell
// command; the server now honours is_first_message too.
func TestRunShellCommand_TitlesFirstMessage(t *testing.T) {
	t.Parallel()

	coord := &titleCoordinator{}
	b, _ := newTestBackend(t)
	ws := &Workspace{
		ID:           uuid.New().String(),
		Path:         t.TempDir(),
		resolvedPath: t.TempDir(),
		clients:      make(map[string]*clientState),
		shutdownFn:   func() {},
		App:          &app.App{AgentCoordinator: coord},
	}
	ws.ctx, ws.cancel = context.WithCancel(b.ctx)
	InsertWorkspaceForTest(b, ws)

	_, err := b.RunShellCommand(t.Context(), ws.ID, proto.ShellCommandRequest{Command: "echo hi"})
	require.NoError(t, err)
	require.Empty(t, coord.prompt, "only a session's first message is titled")

	_, err = b.RunShellCommand(t.Context(), ws.ID, proto.ShellCommandRequest{
		Command:        "echo hi",
		IsFirstMessage: true,
	})
	require.NoError(t, err)
	require.Equal(t, "$ echo hi", coord.prompt)
}

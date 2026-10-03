package model

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/ui/chat"
	"github.com/stretchr/testify/require"
)

type backgroundCommandWorkspace struct {
	*testWorkspace
	backgroundSession string
}

func (w *backgroundCommandWorkspace) AgentBackground(id string) bool {
	w.backgroundSession = id
	return true
}

func TestCtrlBStillBackgroundsCommand(t *testing.T) {
	t.Parallel()
	u := newFrameTestUI(t)
	w := &backgroundCommandWorkspace{testWorkspace: u.com.Workspace.(*testWorkspace)}
	u.com.Workspace = w
	u.session = &session.Session{ID: "background-session"}
	u.chat.SetAgentBusy(true)
	u.chat.SetCanBackground(true)
	u.chat.SetMessages(chat.NewToolMessageItem(u.com.Styles, "assistant", message.ToolCall{
		ID: "running", Name: "bash", Input: `{"command":"sleep 10"}`, Finished: true,
	}, nil, false, ""))
	require.NotNil(t, u.chat.BackgroundableGroup())
	u.handleKeyPressMsg(tea.KeyPressMsg{Code: 'b', Mod: tea.ModCtrl})
	require.Equal(t, u.session.ID, w.backgroundSession)
	require.Nil(t, u.chat.BackgroundableGroup())
	require.False(t, u.forceCompactMode)
	require.Empty(t, w.compactCalls)
}

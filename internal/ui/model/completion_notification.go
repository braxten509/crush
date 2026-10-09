package model

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/agent/notify"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/notification"
)

type agentFinishedMsg struct {
	notification notify.Notification
}

// checkAgentFinished probes off the UI thread: a completion event can sit
// in the event queue while a new turn starts. Cached busy state cannot
// decide whether the completion sound is still appropriate.
func (m *UI) checkAgentFinished(n notify.Notification) tea.Cmd {
	ws := m.com.Workspace
	return func() tea.Msg {
		if ws.AgentIsSessionBusy(n.SessionID) || ws.AgentQueuedPrompts(n.SessionID) > 0 {
			return nil
		}
		return agentFinishedMsg{notification: n}
	}
}

func (m *UI) handleAgentFinished(msg agentFinishedMsg) tea.Cmd {
	n := msg.notification
	if m.hasSession() && m.session.ID == n.SessionID {
		common.StopTurn()
		m.turnOutcome = tea.ProgramStateDone
	}
	return tea.Batch(
		m.guardCompletionAlert(n.SessionID, m.playNotificationSound(notification.SoundComplete)),
		m.guardCompletionAlert(n.SessionID, m.sendNotification(notification.Notification{
			Title:   "Crush is waiting...",
			Message: fmt.Sprintf("Agent's turn completed in \"%s\"", n.SessionTitle),
		})),
	)
}

// guardCompletionAlert checks again when the command executes: a new turn can
// start after the completion event was checked but before its sound is played.
func (m *UI) guardCompletionAlert(sessionID string, alert tea.Cmd) tea.Cmd {
	if alert == nil {
		return nil
	}
	ws := m.com.Workspace
	return func() tea.Msg {
		if ws.AgentIsSessionBusy(sessionID) || ws.AgentQueuedPrompts(sessionID) > 0 {
			return nil
		}
		return alert()
	}
}

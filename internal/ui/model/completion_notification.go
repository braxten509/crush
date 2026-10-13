package model

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/agent/notify"
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
		m.turnOutcome = tea.ProgramStateDone
	}
	return tea.Batch(
		m.guardCompletionAlert(n.SessionID, m.playNotificationSound(notification.SoundComplete)),
		m.guardCompletionAlert(n.SessionID, m.sendNotification(finishedNotification(n.SessionTitle))),
	)
}

// finishedNotification is the desktop notification for a finished turn.
func finishedNotification(sessionTitle string) notification.Notification {
	return notification.Notification{
		Title:   "Crush finished",
		Message: fmt.Sprintf("\"%s\" is done", sessionTitle),
	}
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

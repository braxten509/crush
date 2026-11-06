package model

import "github.com/charmbracelet/crush/internal/ui/common"

// stopTurnClock ignores terminal events for other queued submissions.
func (m *UI) stopTurnClock(sessionID, submissionID string) {
	active, ok := m.turnSubmissions[sessionID]
	if !ok || (submissionID != "" && active != "" && submissionID != active) {
		return
	}
	common.StopTurn(sessionID)
	delete(m.turnSubmissions, sessionID)
}

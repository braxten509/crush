package model

import (
	"errors"
	"testing"

	"github.com/charmbracelet/crush/internal/agent/notify"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

// Prompt reconciliation is covered in pending_prompt_test.go; held prompts
// and Working-row handoff are covered in held_prompt_test.go and
// submitted_status_test.go.
func TestTurnClockSurvivesQueuedPromptAndHandoff(t *testing.T) {
	u, _ := newSubmissionUI()
	u.session.ID = "turn-clock-queue"
	t.Cleanup(func() { common.StopTurn(u.session.ID) })
	u.chat.SetSize(100, 30)
	u.sendMessage("first")
	first := u.queue.pending[0].Clone()
	require.NotEmpty(t, common.Elapsed(u.session.ID))
	require.Contains(t, ansi.Strip(u.chat.list.Render()), "0s")
	u.sendMessage("queued")
	second := u.queue.pending[1].ID
	require.Equal(t, first.ID, u.turnSubmissions[u.session.ID])
	u.handleWorkspaceEvent(pubsub.Event[notify.RunComplete]{Payload: notify.RunComplete{SessionID: u.session.ID, SubmissionID: second, Cancelled: true}})
	require.NotEmpty(t, common.Elapsed(u.session.ID), "a canceled queued prompt must not stop the active clock")
	first.ID = "saved"
	u.appendSessionMessage(first)
	u.appendSessionMessage(message.Message{ID: "assistant", Role: message.Assistant, SessionID: u.session.ID})
	require.Contains(t, ansi.Strip(u.chat.list.Render()), "0s", "the clock survives handoff to the response")
	u.handleWorkspaceEvent(pubsub.Event[notify.RunComplete]{Payload: notify.RunComplete{SessionID: u.session.ID, SubmissionID: u.turnSubmissions[u.session.ID]}})
	require.Empty(t, common.Elapsed(u.session.ID))
}

func TestTurnClockStopsOffSessionForTerminalEvents(t *testing.T) {
	for _, outcome := range []notify.RunComplete{{}, {Error: "failed"}, {Cancelled: true}} {
		u, _ := newSubmissionUI()
		u.session.ID = "turn-clock-background"
		u.sendMessage("first")
		active := u.turnSubmissions[u.session.ID]
		u.session.ID = "another-session"
		outcome.SessionID, outcome.SubmissionID = "turn-clock-background", active
		u.handleWorkspaceEvent(pubsub.Event[notify.RunComplete]{Payload: outcome})
		require.Empty(t, common.Elapsed("turn-clock-background"))
	}
}

func TestTurnClockStopsOnSubmissionFailureAndCancel(t *testing.T) {
	u, _ := newSubmissionUI()
	u.session.ID = "turn-clock-failure"
	u.sendMessage("first")
	id := u.queue.pending[0].ID
	u.handleRunSubmitted(agentRunSubmittedMsg{submissionID: id, dispatchID: id, err: errors.New("failed")})
	require.Empty(t, common.Elapsed(u.session.ID))
	warmCaches(u, false)
	u.sendMessage("second")
	require.NotEmpty(t, common.Elapsed(u.session.ID))
	u.interruptAgent()
	require.Empty(t, common.Elapsed(u.session.ID))
}

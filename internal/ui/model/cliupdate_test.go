package model

import (
	"testing"

	"github.com/charmbracelet/crush/internal/cliupdate"
	"github.com/charmbracelet/crush/internal/question"
	"github.com/stretchr/testify/require"
)

// Another client resolving a pending agent question must not close the CLI
// update prompt, which isn't one of the question service's forms.
func TestCLIUpdatePromptSurvivesQuestionNotification(t *testing.T) {
	t.Parallel()
	m := newFrameTestUI(t)
	m.openCLIUpdateForm([]cliupdate.Update{{Name: "codex", Current: "1.0.0", Latest: "1.1.0"}})
	require.True(t, m.cliUpdatePromptOpen())

	m.handleQuestionNotification(question.Notification{BatchID: "other"})
	require.True(t, m.cliUpdatePromptOpen())
}

// An agent question takes the editor from the CLI update prompt, and the
// offer moves to the status bar instead of vanishing.
func TestAgentQuestionMovesCLIUpdatePromptToStatus(t *testing.T) {
	t.Parallel()
	m := newFrameTestUI(t)
	m.openCLIUpdateForm([]cliupdate.Update{{Name: "codex", Current: "1.0.0", Latest: "1.1.0"}})

	cmd := m.openBatchFormDialog(question.Request{
		ID:        "agent",
		Questions: []question.Question{{ID: "q1", Type: question.TypeYesNo, Text: "Proceed?"}},
	})
	require.NotNil(t, cmd)
	require.False(t, m.cliUpdatePromptOpen())
	require.NotNil(t, m.activeInline)
	require.Contains(t, m.status.msg.Msg, "codex 1.1.0 is available")
}

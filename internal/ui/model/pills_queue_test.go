package model

import (
	"strings"
	"testing"

	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

func TestQueueRowRendersSubagentReport(t *testing.T) {
	t.Parallel()

	u := newTestUI()
	report := message.SubAgentReport{
		Label:  "nancy-roadrunner-recheck",
		Output: "Verification complete.\n\nFinal verdict below.",
	}
	rows := queueRows([]string{report.PromptText()}, u.com.Styles, 120)
	require.Len(t, rows, 1)
	require.Contains(t, ansi.Strip(rows[0]), "Subagent Report → Nancy roadrunner-recheck")
}

func TestQueueRowKeepsMultilinePromptOnOneRow(t *testing.T) {
	t.Parallel()

	u := newTestUI()
	rows := queueRows([]string{"first\nsecond", "third"}, u.com.Styles, 120)
	require.Len(t, rows, 2)
	require.NotContains(t, rows[0], "\n")
	require.True(t, strings.Contains(ansi.Strip(rows[0]), "first second"))
}

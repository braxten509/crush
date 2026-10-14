package model

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/session"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

func TestSkillTagsSitAfterModeBadge(t *testing.T) {
	t.Parallel()
	ws := newLoadWorkspace()
	u := newLoadTestUI(t, ws)
	u.state = uiChat
	u.forceCompactMode, u.isCompact = true, true
	u.session.KeptSkills = []session.KeptSkill{{ID: "/s/deep/SKILL.md", Name: "deep-debugging"}, {ID: "/s/teach/SKILL.md", Name: "teach"}}
	u.status.SetMode(uiInputModeCode, true)
	u.updateLayoutAndSize()

	footer := ansi.Strip(u.composerFooter(120))
	require.NotContains(t, footer, "teach", "the skills no longer push the composer details aside")
	require.Equal(t, 0, u.attachments.Count(), "skills are not chips in the attachment row")

	u.isCompact = false
	require.Empty(t, u.composerFooter(120), "beside the sidebar the line under the composer is empty")
	u.isCompact = true
	u.updateLayoutAndSize()

	scr := uv.NewScreenBuffer(u.layout.status.Dx(), 1)
	u.status.Draw(scr, uv.Rect(0, 0, u.layout.status.Dx(), 1))
	row := strings.TrimRight(ansi.Strip(scr.Render()), " ")
	require.True(t, strings.HasPrefix(row, "  YOLO MODE  deep-debugging ✕  teach ✕"), row)

	x := ansi.StringWidth(row[:strings.Index(row, "teach ✕")]) + ansi.StringWidth("teach ")
	cmd, handled := u.clickComposer(tea.MouseClickMsg{Button: tea.MouseLeft, X: u.layout.status.Min.X + x, Y: u.layout.status.Min.Y})
	require.True(t, handled)
	require.NotNil(t, cmd, "clicking a skill's × turns it off")
	_, handled = u.clickComposer(tea.MouseClickMsg{Button: tea.MouseLeft, X: u.layout.status.Min.X + x - 3, Y: u.layout.status.Min.Y})
	require.False(t, handled, "clicking its name does nothing")
}

func TestTodoSectionHeader(t *testing.T) {
	t.Parallel()
	sty := newTestUI().com.Styles
	todos := []session.Todo{
		{Content: "Restart app", Status: session.TodoStatusCompleted},
		{Content: "Reproduce bug", ActiveForm: "Reproducing bug", Status: session.TodoStatusInProgress},
		{Content: "Find cause", Status: session.TodoStatusPending},
	}
	open := ansi.Strip(todoHeader(todos, "*", true, sty, 60))
	require.True(t, strings.HasPrefix(open, "TO-DO 1/3 "), open)
	require.True(t, strings.HasSuffix(open, "ctrl+t hide"), open)
	require.Equal(t, 60, ansi.StringWidth(open), "the hint sits at the right edge")

	closed := ansi.Strip(todoHeader(todos, "*", false, sty, 60))
	require.True(t, strings.HasPrefix(closed, "TO-DO 1/3 · * Reproducing bug"), closed)
	require.True(t, strings.HasSuffix(closed, "ctrl+t show"), closed)

	tight := ansi.Strip(todoHeader(todos, "*", false, sty, 30))
	require.LessOrEqual(t, ansi.StringWidth(tight), 30)
	require.Contains(t, tight, "…")
	require.True(t, strings.HasSuffix(tight, "ctrl+t show"), "the hint is kept when the step is cut short")
}

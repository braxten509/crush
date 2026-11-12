package model

import (
	"github.com/charmbracelet/crush/internal/permission"
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
	u.status.SetMode(uiInputModeCode, permission.PermissionModeYolo)
	u.updateLayoutAndSize()

	require.Equal(t, 0, u.attachments.Count(), "skills are not chips in the attachment row")

	scr := uv.NewScreenBuffer(u.layout.status.Dx(), 1)
	u.status.Draw(scr, uv.Rect(0, 0, u.layout.status.Dx(), 1))
	row := strings.TrimRight(ansi.Strip(scr.Render()), " ")
	closeAt := strings.LastIndex(row, "✕")
	require.GreaterOrEqual(t, closeAt, 0, "the last skill has a ✕: %q", row)

	x := ansi.StringWidth(row[:closeAt])
	cmd, handled := u.clickComposer(tea.MouseClickMsg{Button: tea.MouseLeft, X: u.layout.status.Min.X + x, Y: u.layout.status.Min.Y})
	require.True(t, handled)
	require.NotNil(t, cmd, "clicking a skill's × turns it off")
	_, handled = u.clickComposer(tea.MouseClickMsg{Button: tea.MouseLeft, X: u.layout.status.Min.X + x - 3, Y: u.layout.status.Min.Y})
	require.False(t, handled, "clicking its name does nothing")
}

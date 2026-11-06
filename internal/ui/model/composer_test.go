package model

import (
	"testing"

	"charm.land/bubbles/v2/textarea"
	"github.com/charmbracelet/crush/internal/permission"
	"github.com/stretchr/testify/require"
)

func TestComposerPlaceholder(t *testing.T) {
	t.Parallel()

	c := composer{readyPlaceholder: "Ready!", workingPlaceholder: "Working!"}
	normal, yolo, sysadmin := permission.PermissionModeNormal, permission.PermissionModeYolo, permission.PermissionModeSysadmin
	require.Equal(t, "Ready!", c.placeholder(false, false, normal))
	require.Equal(t, "Working!", c.placeholder(true, false, normal))
	require.Equal(t, "Let's plan", c.placeholder(false, true, normal))
	require.Equal(t, "Working!", c.placeholder(true, true, normal), "busy wins over planning")
	require.Equal(t, "Go crazy", c.placeholder(true, false, yolo), "YOLO wins over busy")
	require.Equal(t, "Sysadmin mode!", c.placeholder(true, false, sysadmin), "sysadmin wins over busy")
	require.Equal(t, "Let's plan", c.placeholder(false, true, yolo), "planning ignores YOLO")

	c.bangMode = true
	require.Equal(t, "Run a shell command", c.placeholder(true, true, yolo))
}

func TestComposerHistoryDraft(t *testing.T) {
	t.Parallel()

	c := composer{textarea: textarea.New()}
	c.promptHistory.index = 2
	c.textarea.SetValue("new")
	c.updateHistoryDraft("new")
	require.Equal(t, 2, c.promptHistory.index, "no change, no draft")
	c.updateHistoryDraft("old")
	require.Equal(t, "new", c.promptHistory.draft)
	require.Equal(t, -1, c.promptHistory.index)

	c.historyReset()
	require.Empty(t, c.promptHistory.draft)
	require.Equal(t, -1, c.promptHistory.index)
}

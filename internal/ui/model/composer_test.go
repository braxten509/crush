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
	plan := c.placeholder(false, true, normal)
	require.NotEqual(t, "Ready!", plan, "planning has its own placeholder")
	require.Equal(t, "Working!", c.placeholder(true, true, normal), "busy wins over planning")
	yoloIdle := c.placeholder(false, false, yolo)
	require.NotEqual(t, "Working!", yoloIdle)
	require.Equal(t, yoloIdle, c.placeholder(true, false, yolo), "YOLO wins over busy")
	sysadminIdle := c.placeholder(false, false, sysadmin)
	require.NotEqual(t, "Working!", sysadminIdle)
	require.Equal(t, sysadminIdle, c.placeholder(true, false, sysadmin), "sysadmin wins over busy")
	require.Equal(t, plan, c.placeholder(false, true, yolo), "planning ignores YOLO")

	c.bangMode = true
	shell := c.placeholder(true, true, yolo)
	for _, other := range []string{"Ready!", "Working!", plan, yoloIdle, sysadminIdle} {
		require.NotEqual(t, other, shell, "shell mode has its own placeholder")
	}
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

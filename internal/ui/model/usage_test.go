package model

import (
	"errors"
	"testing"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/ui/dialog"
	"github.com/stretchr/testify/require"
)

func TestUsageRefreshIsOffThreadAndIgnoresClosedDialog(t *testing.T) {
	t.Parallel()
	u := newLoadTestUI(t, newLoadWorkspace())
	d := dialog.NewUsage(u.com, dialog.UsageData{Kind: config.TypeClaudeCode})
	u.dialog.OpenDialog(d)
	cmd := u.refreshUsage(d)
	require.NotNil(t, cmd)
	require.True(t, d.Data().Refreshing)
	require.Nil(t, u.refreshUsage(d), "repeat refresh must not start another request")
	u.applyUsageRefresh(usageRefreshedMsg{dialog: d, err: errors.New("offline")})
	require.False(t, d.Data().Refreshing)
	require.Equal(t, "offline", d.Data().Error)
	u.dialog.CloseDialog(dialog.UsageID)
	u.applyUsageRefresh(usageRefreshedMsg{dialog: d})
	require.Equal(t, "offline", d.Data().Error, "a late result must not update a closed dialog")
}

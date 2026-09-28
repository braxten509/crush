package dialog

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/stretchr/testify/require"
)

// The Docker MCP check finishes while the user types. It used to clear the
// filter, so "/remot" + check + "e" filtered on "e" alone and enter quit.
func TestCommandsRefreshKeepsTheFilter(t *testing.T) {
	t.Parallel()
	sty := styles.CharmtonePantera()
	com := &common.Common{Workspace: fastModeWorkspace{cfg: &config.Config{}}, Styles: &sty}
	c, err := NewCommands(com, "", false, false, false, nil, nil)
	require.NoError(t, err)

	for _, r := range "remot" {
		c.HandleMsg(keyMsg(r))
	}
	c.HandleMsg(dockerMCPAvailabilityCheckedMsg{available: false})
	require.Equal(t, "remot", c.input.Value())
	c.HandleMsg(keyMsg('e'))

	require.Equal(t, ActionOpenDialog{RemoteID}, c.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEnter}))
}

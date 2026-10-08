package dialog

import (
	"image"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/remote"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/styles"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/stretchr/testify/require"
)

func TestRemoteSetupActions(t *testing.T) {
	t.Parallel()
	sty := styles.CharmtonePantera()
	d := NewRemoteSetup(&common.Common{Styles: &sty})
	enter := tea.KeyPressMsg{Code: tea.KeyEnter}
	require.IsType(t, ActionRemoteSetupClose{}, d.HandleMsg(enter), "checking must not start an installation")
	d.SetInfo(remote.SetupInfo{State: remote.SetupMissing, Action: "install", Button: "Install Tailscale"})
	require.Equal(t, ActionRemoteSetup{Action: "install"}, d.HandleMsg(enter))
	d.HandleMsg(tea.KeyPressMsg{Code: tea.KeyTab})
	require.Equal(t, ActionRemoteSetup{Action: "check"}, d.HandleMsg(enter))
	d.SetInfo(remote.SetupInfo{State: remote.SetupMissing, Action: "install", Button: "Install Tailscale"})
	require.Equal(t, ActionRemoteSetup{Action: "check"}, d.HandleMsg(enter), "polling must keep the user's selection")
	d.HandleMsg(tea.KeyPressMsg{Code: tea.KeyTab})
	require.IsType(t, ActionRemoteSetupClose{}, d.HandleMsg(enter))
	d.SetInfo(remote.SetupInfo{State: remote.SetupApproval})
	require.Equal(t, ActionRemoteSetup{Action: "check"}, d.HandleMsg(enter))
	require.IsType(t, ActionRemoteSetupClose{}, d.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEscape}))
}

func TestRemoteSetupRenderingAndMouse(t *testing.T) {
	t.Parallel()
	for _, width := range []int{80, 41, 32} {
		sty := styles.CharmtonePantera()
		d := NewRemoteSetup(&common.Common{Styles: &sty})
		d.SetInfo(remote.SetupInfo{State: remote.SetupMissing, Title: "Install Tailscale", Detail: "Tailscale connects this computer privately to Pocket Agents. The official installer may ask for your computer password.", Action: "install", Button: "Install Tailscale"})
		scr := uv.NewScreenBuffer(width, 30)
		d.Draw(scr, image.Rect(0, 0, width, 30))
		text := screenText(scr)
		if width == 80 || width == 32 {
			t.Logf("Terminal width %d:\n%s", width, text)
		}
		require.Contains(t, text, "Install Tailscale")
		require.Contains(t, text, "Cancel")
		require.Len(t, d.buttons, 3)
		for i, rect := range d.buttons {
			var label strings.Builder
			for x := rect.Min.X; x < rect.Max.X; x++ {
				if c := scr.CellAt(x, rect.Min.Y); c != nil {
					label.WriteString(c.Content)
				}
			}
			require.Contains(t, label.String(), d.choices()[i], "width %d button %d must match its hit box", width, i)
			action := d.HandleMsg(tea.MouseClickMsg(tea.Mouse{X: rect.Min.X, Y: rect.Min.Y, Button: tea.MouseLeft}))
			if i == 2 {
				require.IsType(t, ActionRemoteSetupClose{}, action)
			} else {
				require.IsType(t, ActionRemoteSetup{}, action)
			}
		}
	}
}

package dialog

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/styles"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

func TestSubAgentInstructionsDialog(t *testing.T) {
	t.Parallel()
	sty := styles.CharmtonePantera()
	cfg := ultracodeConfig(config.TypeCodexCLI, nil, false)
	cfg.Options = &config.Options{TUI: &config.TUIOptions{}}
	com := &common.Common{Workspace: fastModeWorkspace{cfg: cfg}, Styles: &sty}

	commands := &Commands{com: com}
	var command *CommandItem
	for _, item := range commands.defaultCommands() {
		if item.ID() == SubAgentInstructionsID {
			command = item
		}
	}
	require.NotNil(t, command, "Ctrl+P lists the command")
	require.Equal(t, "Sub-agent Instructions", command.title)
	require.Equal(t, ActionOpenDialog{SubAgentInstructionsID}, command.Action())

	d := NewSubAgentInstructions(com, "- UI work: Opus, high")
	for _, size := range [][2]int{{100, 30}, {50, 14}, {30, 10}} {
		scr := uv.NewScreenBuffer(size[0], size[1])
		cursor := d.Draw(scr, scr.Bounds())
		out := ansi.Strip(scr.Render())
		require.Contains(t, out, "Sub-agent")
		require.Contains(t, out, "UI work")
		require.NotNil(t, cursor)
		require.Less(t, cursor.X, size[0])
		require.Less(t, cursor.Y, size[1])
	}

	d.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEnter})
	for _, r := range "- Reviews" {
		d.HandleMsg(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	require.Equal(t, ActionSaveSubAgentInstructions{Text: "- UI work: Opus, high\n- Reviews"},
		d.HandleMsg(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}), "enter adds a line and ctrl+s saves it all")
	require.Equal(t, ActionClose{}, d.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEscape}))
}

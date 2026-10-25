package dialog

import (
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/stretchr/testify/require"
)

func TestAutocompactGlobalDialog(t *testing.T) {
	t.Parallel()
	sty := styles.CharmtonePantera()
	com := ultracodeCommon(&sty, config.TypeCodexCLI, nil, false)
	cfg := com.Config()
	cfg.Options = &config.Options{AutoCompactTokenLimit: 275000, TUI: &config.TUIOptions{}}
	d, err := NewAutocompact(com)
	require.NoError(t, err)
	require.Equal(t, "275000", d.input.Value())
	for _, width := range []int{80, 40, 30} {
		scr := uv.NewScreenBuffer(width, 18)
		cursor := d.Draw(scr, scr.Bounds())
		out := ansi.Strip(scr.Render())
		require.Contains(t, out, "Fallback compaction")
		require.Contains(t, out, "275000")
		require.NotNil(t, cursor)
		require.Less(t, cursor.X, width)
		require.Less(t, cursor.Y, 18)
	}
	commands := &Commands{com: com}
	var command *CommandItem
	for _, item := range commands.defaultCommands() {
		if item.ID() == AutocompactID {
			command = item
		}
	}
	require.NotNil(t, command)
	require.Equal(t, "Fallback compaction", command.title)
	require.Equal(t, ActionOpenDialog{AutocompactID}, command.Action())
	enter := tea.KeyPressMsg{Code: tea.KeyEnter}
	for _, bad := range []string{"", "zero", "-1", "999", "1.5", "999999999999999999999"} {
		d.input.SetValue(bad)
		require.Nil(t, d.HandleMsg(enter))
		require.NotEmpty(t, d.errorText)
	}
	d.input.SetValue("400,000")
	require.Equal(t, ActionSetAutocompact{Tokens: 400000}, d.HandleMsg(enter))
	require.Equal(t, ActionClose{}, d.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEscape}))
	require.EqualValues(t, 275000, cfg.Options.AutoCompactTokenLimit, "the dialog only emits a save action")
	// The value belongs to Options, not to a model.
	d, err = NewAutocompact(com)
	require.NoError(t, err)
	require.Equal(t, "275000", d.input.Value())
}

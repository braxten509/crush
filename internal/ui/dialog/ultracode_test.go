package dialog

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/csync"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/stretchr/testify/require"
)

// ultracodeCommon is a Common whose shown chat runs model "m" of a kind CLI.
func ultracodeCommon(sty *styles.Styles, kind catwalk.Type, levels []string, on bool) *common.Common {
	cfg := &config.Config{
		Providers: csync.NewMapFrom(map[string]config.ProviderConfig{
			"cli": {Type: kind, Models: []catwalk.Model{{ID: "m", CanReason: true, ReasoningLevels: levels}}},
		}),
	}
	return chatCommon(cfg, sty, config.SelectedModel{Provider: "cli", Model: "m", ReasoningEffort: "high", Ultracode: on})
}

func TestUltracodeCommand(t *testing.T) {
	t.Parallel()
	efforts := []string{"low", "high", "max"}
	for _, tc := range []struct {
		name   string
		kind   catwalk.Type
		levels []string
		on     bool
		title  string
	}{
		{"claude off", config.TypeClaudeCode, efforts, false, "Turn On Ultracode"},
		{"claude on", config.TypeClaudeCode, efforts, true, "Turn Off Ultracode"},
		{"claude without effort", config.TypeClaudeCode, nil, false, ""},
		{"codex", config.TypeCodexCLI, efforts, false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			sty := styles.CharmtonePantera()
			commands := &Commands{com: ultracodeCommon(&sty, tc.kind, tc.levels, tc.on)}
			var item *CommandItem
			for _, it := range commands.defaultCommands() {
				if it.ID() == "toggle_ultracode" {
					item = it
				}
			}
			if tc.title == "" {
				require.Nil(t, item)
				return
			}
			require.NotNil(t, item)
			require.Equal(t, tc.title, item.title)
			require.IsType(t, ActionToggleUltracode{}, item.Action())
		})
	}
}

func TestReasoningDialogUltracodeSwitch(t *testing.T) {
	t.Parallel()
	sty := styles.CharmtonePantera()
	enter := tea.KeyPressMsg{Code: tea.KeyEnter}
	tab := tea.KeyPressMsg{Code: tea.KeyTab}

	r, err := NewReasoning(ultracodeCommon(&sty, config.TypeClaudeCode, []string{"low", "high", "max"}, false))
	require.NoError(t, err)
	require.Nil(t, r.HandleMsg(tab))
	got := r.HandleMsg(enter).(ActionSelectReasoningEffort)
	require.Equal(t, "high", got.Effort)
	require.NotNil(t, got.Ultracode)
	require.True(t, *got.Ultracode)
	r.HandleMsg(tab)
	require.False(t, *r.HandleMsg(enter).(ActionSelectReasoningEffort).Ultracode)

	// Models without Ultracode leave it alone.
	r, err = NewReasoning(ultracodeCommon(&sty, config.TypeCodexCLI, []string{"low", "high"}, false))
	require.NoError(t, err)
	r.HandleMsg(tab)
	require.Nil(t, r.HandleMsg(enter).(ActionSelectReasoningEffort).Ultracode)
}

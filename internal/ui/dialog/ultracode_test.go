package dialog

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/catwalk/v3/pkg/catwalk"
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
			"cli": {Type: kind, Models: []catwalk.Model{{ID: "m", Reasoning: catwalk.Reasoning{Thinking: catwalk.ThinkingToggleable, EffortLevels: catwalk.NewEffortLevels(levels...)}}}},
		}),
	}
	return chatCommon(cfg, sty, config.SelectedModel{Provider: "cli", Model: "m", ReasoningEffort: "high", Ultracode: on})
}

// ultracodeCommand returns the Ultracode toggle offered for a chat, or nil
// when none is offered.
func ultracodeCommand(kind catwalk.Type, levels []string, on bool) *CommandItem {
	sty := styles.CharmtonePantera()
	commands := &Commands{com: ultracodeCommon(&sty, kind, levels, on)}
	var item *CommandItem
	for _, it := range commands.defaultCommands() {
		if it.ID() == "toggle_ultracode" {
			item = it
		}
	}
	return item
}

func TestUltracodeCommand(t *testing.T) {
	t.Parallel()
	efforts := []string{"low", "high", "max"}
	off := ultracodeCommand(config.TypeClaudeCode, efforts, false)
	on := ultracodeCommand(config.TypeClaudeCode, efforts, true)
	require.NotNil(t, off)
	require.NotNil(t, on)
	require.IsType(t, ActionToggleUltracode{}, off.Action())
	require.IsType(t, ActionToggleUltracode{}, on.Action())
	require.NotEqual(t, off.title, on.title, "the title tells on from off")

	require.Nil(t, ultracodeCommand(config.TypeClaudeCode, nil, false), "claude without effort levels")
	require.Nil(t, ultracodeCommand(config.TypeCodexCLI, efforts, false), "codex")
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

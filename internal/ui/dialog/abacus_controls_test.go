package dialog

import (
	"testing"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/csync"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/stretchr/testify/require"
)

func TestAbacusCommandControlsMatchModelCapabilities(t *testing.T) {
	for _, test := range []struct {
		model                  catwalk.Model
		effort, thinking, fast bool
	}{
		{catwalk.Model{ID: "claude-opus-5-5-thinking", CanReason: true, ReasoningLevels: []string{"low", "medium", "high", "xhigh", "max"}, DefaultReasoningEffort: "medium"}, true, false, false},
		{catwalk.Model{ID: "gpt-6.1-sol", CanReason: true, ReasoningLevels: []string{"low", "medium", "high", "xhigh", "max"}, DefaultReasoningEffort: "medium"}, true, false, true},
		{catwalk.Model{ID: "claude-haiku-4-5-20251001", CanReason: true}, false, true, false},
		{catwalk.Model{ID: "unknown-always-thinking", CanReason: true}, false, false, false},
	} {
		t.Run(test.model.ID, func(t *testing.T) {
			cfg := &config.Config{
				Agents:    map[string]config.Agent{config.AgentCoder: {Model: config.SelectedModelTypeLarge}},
				Models:    map[config.SelectedModelType]config.SelectedModel{config.SelectedModelTypeLarge: {Provider: "abacus", Model: test.model.ID}},
				Providers: csync.NewMapFrom(map[string]config.ProviderConfig{"abacus": {ID: "abacus", Type: catwalk.TypeOpenAICompat, Models: []catwalk.Model{test.model}}}),
			}
			sty := styles.CharmtonePantera()
			com := &common.Common{Workspace: fastModeWorkspace{cfg: cfg}, Styles: &sty}
			commands := &Commands{com: com}
			present := map[string]bool{}
			for _, command := range commands.defaultCommands() {
				present[command.ID()] = true
			}
			require.Equal(t, test.effort, present["select_reasoning_effort"])
			require.Equal(t, test.thinking, present["toggle_thinking"])
			require.Equal(t, test.fast, present["toggle_fast_mode"])
			if test.effort {
				dialog, err := NewReasoning(com)
				require.NoError(t, err)
				require.Equal(t, test.model.DefaultReasoningEffort, dialog.list.SelectedItem().(*ReasoningItem).effort)
			}
		})
	}
}

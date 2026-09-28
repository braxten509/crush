package dialog

import (
	"testing"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/csync"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/charmbracelet/crush/internal/workspace"
	"github.com/stretchr/testify/require"
)

type fastModeWorkspace struct {
	workspace.Workspace
	cfg *config.Config
}

func (w fastModeWorkspace) Config() *config.Config { return w.cfg }

func TestFastModeCommand(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		kind  catwalk.Type
		tier  string
		title string
	}{
		{config.TypeCodexCLI, "", "Enable FAST Mode"},
		{config.TypeCodexCLI, "fast", "Disable FAST Mode"},
		{config.TypeCodexCLI, "default", "Enable FAST Mode"},
		{config.TypeClaudeCode, "fast", ""},
	} {
		t.Run(string(tc.kind)+"/"+tc.tier, func(t *testing.T) {
			t.Parallel()
			cfg := &config.Config{
				Agents: map[string]config.Agent{config.AgentCoder: {Model: config.SelectedModelTypeLarge}},
				Models: map[config.SelectedModelType]config.SelectedModel{
					config.SelectedModelTypeLarge: {Provider: "cli", Model: "m", ServiceTier: tc.tier},
				},
				Providers: csync.NewMapFrom(map[string]config.ProviderConfig{
					"cli": {Type: tc.kind, Models: []catwalk.Model{{ID: "m"}}},
				}),
			}
			sty := styles.CharmtonePantera()
			commands := &Commands{com: &common.Common{Workspace: fastModeWorkspace{cfg: cfg}, Styles: &sty}}
			var fast *CommandItem
			for _, item := range commands.defaultCommands() {
				if item.ID() == "toggle_fast_mode" {
					fast = item
				}
			}
			if tc.title == "" {
				require.Nil(t, fast)
				return
			}
			require.NotNil(t, fast)
			require.Equal(t, tc.title, fast.title)
			require.IsType(t, ActionToggleFastMode{}, fast.Action())
			require.Contains(t, fast.Filter(), "fast")
		})
	}
}

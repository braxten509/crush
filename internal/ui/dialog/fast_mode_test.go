package dialog

import (
	"testing"

	"charm.land/catwalk/v3/pkg/catwalk"
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

// chatCommon is a Common whose shown chat runs on model.
func chatCommon(cfg *config.Config, sty *styles.Styles, model config.SelectedModel) *common.Common {
	return &common.Common{
		Workspace:  fastModeWorkspace{cfg: cfg},
		Styles:     sty,
		ShownModel: func() (config.SelectedModel, bool) { return model, model.Provider != "" },
	}
}

// fastCommand returns the FAST toggle offered for a chat on kind/model with
// the given service tier, or nil when none is offered.
func fastCommand(t *testing.T, kind catwalk.Type, model, tier string) *CommandItem {
	t.Helper()
	cfg := &config.Config{
		Providers: csync.NewMapFrom(map[string]config.ProviderConfig{
			"cli": {Type: kind, Models: []catwalk.Model{{ID: model}}},
		}),
	}
	sty := styles.CharmtonePantera()
	commands := &Commands{com: chatCommon(cfg, &sty, config.SelectedModel{Provider: "cli", Model: model, ServiceTier: tier})}
	var fast *CommandItem
	for _, item := range commands.defaultCommands() {
		if item.ID() == "toggle_fast_mode" {
			fast = item
		}
	}
	return fast
}

func TestFastModeCommand(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		kind  catwalk.Type
		model string
	}{
		{config.TypeCodexCLI, "gpt-6.1-sol"},
		{config.TypeClaudeCode, "opus"},
	} {
		t.Run(string(tc.kind), func(t *testing.T) {
			t.Parallel()
			titles := map[string]string{}
			for _, tier := range []string{"", "fast", "default"} {
				fast := fastCommand(t, tc.kind, tc.model, tier)
				require.NotNil(t, fast, tier)
				require.IsType(t, ActionToggleFastMode{}, fast.Action())
				require.Contains(t, fast.Filter(), "fast")
				titles[tier] = fast.title
			}
			require.Equal(t, titles[""], titles["default"], "no tier and the default tier both read as off")
			require.NotEqual(t, titles[""], titles["fast"], "the fast tier reads as on")
		})
	}
	require.Nil(t, fastCommand(t, config.TypeGrokCLI, "grok-4.7", "fast"), "no FAST toggle for other CLIs")
}

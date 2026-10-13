package dialog

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/csync"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/charmbracelet/x/exp/golden"
)

// TestDefaultCommandsList pins the command palette entries, their order,
// shortcuts, aliases and actions, across the session, model and option
// states that add, drop or relabel commands.
func TestDefaultCommandsList(t *testing.T) {
	yes, no := true, false
	reasoningModel := catwalk.Model{ID: "think", CanReason: true, SupportsImages: true}
	effortModel := catwalk.Model{ID: "effort", CanReason: true, ReasoningLevels: []string{"low", "high"}}
	cases := []struct {
		name     string
		cfg      *config.Config
		commands Commands
		editor   string
	}{
		{
			name:     "bare",
			cfg:      &config.Config{},
			commands: Commands{},
		},
		{
			name: "session with thinking model",
			cfg: &config.Config{
				Agents: map[string]config.Agent{config.AgentCoder: {Model: config.SelectedModelTypeLarge}},
				Models: map[config.SelectedModelType]config.SelectedModel{
					config.SelectedModelTypeLarge: {Provider: "p", Model: "think", Think: true},
				},
				Providers: csync.NewMapFrom(map[string]config.ProviderConfig{
					"p": {ID: "p", Models: []catwalk.Model{reasoningModel}},
				}),
				MCP: config.MCPs{config.DockerMCPName: {}},
				Options: &config.Options{
					TUI:              &config.TUIOptions{Transparent: &yes, Mouse: &no, ComposerFocusOnly: true},
					AgentCPUsEnabled: true,
				},
			},
			commands: Commands{sessionID: "s1", hasSession: true, hasTodos: true, hasQueue: true, windowWidth: 200},
			editor:   "vi",
		},
		{
			name: "claude code with effort levels",
			cfg: &config.Config{
				Agents: map[string]config.Agent{config.AgentCoder: {Model: config.SelectedModelTypeLarge}},
				Models: map[config.SelectedModelType]config.SelectedModel{
					config.SelectedModelTypeLarge: {Provider: "cc", Model: "effort", ServiceTier: "fast", Ultracode: true},
				},
				Providers: csync.NewMapFrom(map[string]config.ProviderConfig{
					"cc": {ID: "cc", Type: config.TypeClaudeCode, Models: []catwalk.Model{effortModel}},
				}),
				MCP:     config.MCPs{"other": {}},
				Options: &config.Options{TUI: &config.TUIOptions{Mouse: &yes}},
			},
			commands: Commands{hasSession: true, hasQueue: true, windowWidth: 80, dockerMCPAvailable: &yes},
		},
		{
			name:     "todos only",
			cfg:      &config.Config{},
			commands: Commands{hasTodos: true, dockerMCPAvailable: &no},
		},
	}

	var b strings.Builder
	for _, tc := range cases {
		t.Setenv("EDITOR", tc.editor)
		sty := styles.CharmtonePantera()
		c := tc.commands
		c.com = &common.Common{Workspace: fastModeWorkspace{cfg: tc.cfg}, Styles: &sty}
		fmt.Fprintf(&b, "=== %s\n", tc.name)
		for _, item := range c.defaultCommands() {
			// Whether CPU affinity is offered depends on the platform.
			if item.ID() == "toggle_agent_cpus" {
				continue
			}
			fmt.Fprintf(&b, "%s | %s | %s | %T%+v | %s\n", item.ID(), item.title, item.Shortcut(), item.Action(), item.Action(), item.Filter())
		}
		b.WriteString("\n")
	}
	golden.RequireEqual(t, []byte(b.String()))
}

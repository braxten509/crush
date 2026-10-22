package model

import (
	"testing"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/ui/dialog"
	"github.com/stretchr/testify/require"
)

func effortUI(t *testing.T) (*UI, *prefWorkspace) {
	u, ws := newPrefUI(t)
	levels := []string{"low", "medium", "high", "max"}
	ws.cfg.Providers.Set("p", config.ProviderConfig{ID: "p", Models: []catwalk.Model{
		{ID: "m1", CanReason: true, ReasoningLevels: levels, DefaultReasoningEffort: "high"},
		{ID: "other", CanReason: true, ReasoningLevels: levels, DefaultReasoningEffort: "high"},
	}})
	ws.cfg.Models[config.SelectedModelTypeLarge] = config.SelectedModel{Provider: "p", Model: "other", ReasoningEffort: "low"}
	ws.cfg.Models[config.SelectedModelTypeSmall] = config.SelectedModel{Provider: "p", Model: "other"}
	ws.cfg.ModelEfforts = map[string]string{"p:m1": "max", "p:other": "low"}
	return u, ws
}

// Opening a chat that last used another model brings that model back with
// the effort last used with it, not its default (owner, 2026-10-22).
func TestRestoredModelKeepsItsEffort(t *testing.T) {
	t.Parallel()

	u, ws := effortUI(t)
	runAll(u, u.restoreModelFromSession([]message.Message{{Role: message.Assistant, Provider: "p", Model: "m1"}}))
	require.Equal(t, "max", ws.cfg.Models[config.SelectedModelTypeLarge].ReasoningEffort)
}

// Picking a model in the models dialog does the same.
func TestPickedModelKeepsItsEffort(t *testing.T) {
	t.Parallel()

	u, ws := effortUI(t)
	runAll(u, u.handleDialogAction(dialog.ActionSelectModel{
		Provider:  catwalk.Provider{ID: "p"},
		ModelType: config.SelectedModelTypeLarge,
		Model:     config.SelectedModel{Provider: "p", Model: "m1", ReasoningEffort: "high"},
	}))
	require.Equal(t, "max", ws.cfg.Models[config.SelectedModelTypeLarge].ReasoningEffort)
}

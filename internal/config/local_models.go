package config

import (
	"fmt"
	"log/slog"
	"os"
	"testing"

	"charm.land/catwalk/v3/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/localmodel"
)

// Local weights are a built-in provider, not saved credentials or a dependency
// on the RE sandbox. Explicit-provider sandboxes opt out with the other defaults.
func (c *Config) addLocalModels() {
	if testing.Testing() && os.Getenv("CRUSH_LOCAL_MODEL_DIRS") == "" {
		return
	}
	p, configured := c.Providers.Get(localmodel.ProviderID)
	if p.Disable || (c.Options.DisableDefaultProviders && !configured) {
		return
	}
	models := localmodel.Discover(localmodel.Roots())
	if len(models) == 0 {
		return
	}
	endpoint, key, err := localmodel.Default.Endpoint()
	if err != nil {
		slog.Warn("Could not prepare local model connection", "error", err)
		return
	}
	list := make([]catwalk.Model, 0, len(models))
	for _, model := range models {
		entry := catwalk.Model{ID: model.ID, Name: fmt.Sprintf("%s · %.1f GiB", model.Name, float64(model.Bytes)/(1<<30)), ContextWindow: localmodel.Default.PlannedContext(model), DefaultMaxTokens: 4096}
		entry.Reasoning = localReasoning(model)
		list = append(list, entry)
	}
	c.Providers.Set(localmodel.ProviderID, ProviderConfig{ID: localmodel.ProviderID, Name: "Local models", Type: catwalk.TypeCompletions, BaseURL: endpoint, APIKey: key, Models: list, AutoDiscoverModels: new(false)})
}

// localReasoning offers the effort switch the model's own chat template
// reads; the local relay turns the chosen level into that switch.
func localReasoning(model localmodel.Model) catwalk.Reasoning {
	info, _ := localmodel.ModelInfo(model.Path)
	switch info.Reasoning {
	case "effort":
		levels, fallback := info.Efforts, info.DefaultEffort
		if len(levels) == 0 {
			levels = []string{"low", "medium", "high"}
		}
		if fallback == "" {
			fallback = levels[len(levels)/2]
		}
		return catwalk.Reasoning{Thinking: catwalk.ThinkingToggleable, EffortLevels: catwalk.NewEffortLevels(levels...), DefaultEffortLevel: fallback}
	case "thinking":
		return catwalk.Reasoning{Thinking: catwalk.ThinkingToggleable, EffortLevels: catwalk.NewEffortLevels("off", "on"), DefaultEffortLevel: "on"}
	}
	return catwalk.Reasoning{}
}

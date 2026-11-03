package config

import (
	"fmt"
	"log/slog"
	"testing"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/localmodel"
)

// Local weights are a built-in provider, not saved credentials or a dependency
// on the RE sandbox. Explicit-provider sandboxes opt out with the other defaults.
func (c *Config) addLocalModels() {
	if testing.Testing() || c.Options.DisableDefaultProviders {
		return
	}
	if p, ok := c.Providers.Get(localmodel.ProviderID); ok && p.Disable {
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
		list = append(list, catwalk.Model{ID: model.ID, Name: fmt.Sprintf("%s · %.1f GiB", model.Name, float64(model.Bytes)/(1<<30)), ContextWindow: localmodel.ContextSize, DefaultMaxTokens: 4096})
	}
	c.Providers.Set(localmodel.ProviderID, ProviderConfig{ID: localmodel.ProviderID, Name: "Local models", Type: catwalk.TypeOpenAICompat, BaseURL: endpoint, APIKey: key, Models: list, AutoDiscoverModels: new(false)})
}

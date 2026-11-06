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
		list = append(list, catwalk.Model{ID: model.ID, Name: fmt.Sprintf("%s · %.1f GiB", model.Name, float64(model.Bytes)/(1<<30)), ContextWindow: localmodel.ContextSize, DefaultMaxTokens: 4096})
	}
	c.Providers.Set(localmodel.ProviderID, ProviderConfig{ID: localmodel.ProviderID, Name: "Local models", Type: catwalk.TypeCompletions, BaseURL: endpoint, APIKey: key, Models: list, AutoDiscoverModels: new(false)})
}

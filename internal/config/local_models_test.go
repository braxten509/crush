package config

import (
	"os"
	"path/filepath"
	"testing"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/csync"
	"github.com/charmbracelet/crush/internal/localmodel"
	"github.com/stretchr/testify/require"
)

func TestLocalModelsOptInWithDefaultProvidersDisabled(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Example.gguf"), []byte("GGUFfixture"), 0600))
	t.Setenv("CRUSH_LOCAL_MODEL_DIRS", dir)
	c := &Config{Options: &Options{DisableDefaultProviders: true}, Providers: csync.NewMap[string, ProviderConfig]()}
	c.addLocalModels()
	require.Zero(t, c.Providers.Len(), "isolated explicit-provider configurations must not acquire local access")
	c.Providers.Set(localmodel.ProviderID, ProviderConfig{ID: localmodel.ProviderID, Type: catwalk.TypeOpenAICompat})
	c.addLocalModels()
	p, ok := c.Providers.Get(localmodel.ProviderID)
	require.True(t, ok)
	require.Len(t, p.Models, 1)
	require.Contains(t, p.Models[0].Name, "Example.gguf ·")
	require.NotContains(t, p.Models[0].Name, dir)
	require.NotEmpty(t, p.APIKey)
	require.False(t, *p.AutoDiscoverModels)
	p.Disable = true
	c.Providers.Set(localmodel.ProviderID, p)
	c.addLocalModels()
	p, _ = c.Providers.Get(localmodel.ProviderID)
	require.True(t, p.Disable)
}

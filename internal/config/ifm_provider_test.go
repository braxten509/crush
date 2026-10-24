package config

import (
	"testing"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/config/providers"
	"github.com/stretchr/testify/require"
)

func TestIFMProviderCatalog(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing upstream", true: "already upstream"}[existing], func(t *testing.T) {
			t.Setenv("CRUSH_GLOBAL_DATA", t.TempDir())
			resetProviderState()
			t.Cleanup(resetProviderState)
			catalog := []catwalk.Provider{{ID: "other", Name: "Other"}}
			if existing {
				catalog = append(catalog, catwalk.Provider{ID: "ifm", Name: "Upstream IFM"})
			}
			require.NoError(t, providers.NewCache[[]catwalk.Provider](cachePathFor("providers")).Store(catalog))
			got, err := Providers(&Config{Options: &Options{DisableProviderAutoUpdate: true}})
			require.NoError(t, err)
			var matches []catwalk.Provider
			for _, provider := range got {
				if provider.ID == "ifm" {
					matches = append(matches, provider)
				}
			}
			require.Len(t, matches, 1)
			provider := matches[0]
			if existing {
				require.Equal(t, "Upstream IFM", provider.Name)
				return
			}
			require.Equal(t, "IFM", provider.Name)
			require.Equal(t, catwalk.TypeOpenAICompat, provider.Type)
			require.Equal(t, "https://api.ifm.ai/v1", provider.APIEndpoint)
			require.Equal(t, "$IFM_API_KEY", provider.APIKey)
			require.Len(t, provider.Models, 1)
			model := provider.Models[0]
			require.Equal(t, "IFM/K2-Horizon-375B-A23B", model.ID)
			require.Equal(t, model.ID, provider.DefaultLargeModelID)
			require.Equal(t, model.ID, provider.DefaultSmallModelID)
			require.Equal(t, int64(524288), model.ContextWindow)
			require.True(t, model.CanReason)
			require.Equal(t, []string{"low", "medium", "high"}, model.ReasoningLevels)
			require.Equal(t, "high", model.DefaultReasoningEffort)
		})
	}
}

package config

import (
	"testing"

	"charm.land/catwalk/v3/pkg/catwalk"
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
			require.NotEmpty(t, provider.Models)
			var ids []string
			for _, model := range provider.Models {
				ids = append(ids, model.ID)
			}
			require.Contains(t, ids, provider.DefaultLargeModelID)
		})
	}
}

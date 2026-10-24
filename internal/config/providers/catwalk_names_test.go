package providers

import (
	"testing"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/stretchr/testify/require"
)

func TestPatchCatalogNames(t *testing.T) {
	t.Parallel()

	got := patchCatalog([]catwalk.Provider{
		{ID: catwalk.InferenceKimiCoding, Name: "Kimi Coding"},
		{ID: catwalk.InferenceProviderOpenAI, Name: "OpenAI"},
	})
	require.Equal(t, "Moonshot AI", got[0].Name)
	require.Equal(t, "OpenAI", got[1].Name)
}

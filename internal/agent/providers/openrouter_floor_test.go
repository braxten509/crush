package providers

import (
	"testing"

	"charm.land/catwalk/pkg/catwalk"
	"charm.land/fantasy/providers/openrouter"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/stretchr/testify/require"
)

// OpenRouter hosts a model at several precisions, so a 4-bit host answered
// the owner's prompts without saying so: every request asks for FP8 quality
// or better, and hosts that do not say their precision are allowed too
// (owner, 2026-10-22).
func TestOpenRouterRefusesHostsBelowFP8(t *testing.T) {
	t.Parallel()
	routing := func(p config.ProviderConfig, selected config.SelectedModel) *openrouter.Provider {
		options := Options(catwalk.Model{ID: "deepseek/deepseek-v4.1-flash"}, selected, p)
		parsed, ok := options[openrouter.Name].(*openrouter.ProviderOptions)
		require.True(t, ok)
		return parsed.Provider
	}
	floor := []string{"fp8", "mxfp8", "fp16", "bf16", "fp32", "unknown"}
	openrouterProvider := config.ProviderConfig{ID: "openrouter", Type: openrouter.Name}
	require.Equal(t, floor, routing(openrouterProvider, config.SelectedModel{}).Quantizations)

	// The user's own list of precisions is theirs, not ours.
	own := config.SelectedModel{ProviderOptions: map[string]any{"provider": map[string]any{"quantizations": []any{"fp16"}}}}
	require.Equal(t, []string{"fp16"}, routing(openrouterProvider, own).Quantizations)

	// Switched off leaves OpenRouter's routing as it was.
	off := false
	openrouterProvider.QualityFloor = &off
	require.Empty(t, routing(openrouterProvider, config.SelectedModel{}).Quantizations)
}

// The floor covers both switches: the cheapest host and the quality floor
// share one provider object.
func TestOpenRouterFloorRidesWithTheCheapestHostSwitch(t *testing.T) {
	t.Parallel()
	off := false
	p := config.ProviderConfig{ID: "openrouter", Type: openrouter.Name, CheapestHost: &off}
	options := Options(catwalk.Model{ID: "deepseek/deepseek-v4.1-flash"}, config.SelectedModel{}, p)
	provider := options[openrouter.Name].(*openrouter.ProviderOptions).Provider
	require.Nil(t, provider.Sort, "the cheapest host switch is off")
	require.NotEmpty(t, provider.Quantizations, "the quality floor is still on")
}

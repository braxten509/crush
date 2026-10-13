package app

import (
	"testing"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/stretchr/testify/require"
)

// characterizationProviders is shared in shape by the model-string
// characterization tests in app and cmd so the two implementations can be
// compared case by case.
func characterizationProviders() map[string]config.ProviderConfig {
	return map[string]config.ProviderConfig{
		"openai":     {ID: "openai", Models: []catwalk.Model{{ID: "gpt-4o"}, {ID: "shared"}}},
		"anthropic":  {ID: "anthropic", Models: []catwalk.Model{{ID: "claude-x"}, {ID: "shared"}}},
		"synthetic":  {ID: "synthetic", Models: []catwalk.Model{{ID: "moonshot/kimi-k2"}}},
		"openrouter": {ID: "openrouter", Models: []catwalk.Model{{ID: "anthropic/claude-x"}, {ID: "anthropic/claude-or"}}},
		"off":        {ID: "off", Disable: true, Models: []catwalk.Model{{ID: "off-model"}}},
	}
}

// TestModelStringCharacterization records how the in-process `crush run`
// path resolves --model strings today.
func TestModelStringCharacterization(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in       string
		provider string
		model    string
		errPart  string
	}{
		{in: "gpt-4o", provider: "openai", model: "gpt-4o"},
		{in: "GPT-4O", provider: "openai", model: "gpt-4o"},
		{in: "openai/gpt-4o", provider: "openai", model: "gpt-4o"},
		{in: "OpenAI/gpt-4o", errPart: `large model "OpenAI/gpt-4o" not found`},
		{in: "moonshot/kimi-k2", provider: "synthetic", model: "moonshot/kimi-k2"},
		{in: "synthetic/moonshot/kimi-k2", provider: "synthetic", model: "moonshot/kimi-k2"},
		{in: "anthropic/claude-x", provider: "anthropic", model: "claude-x"},
		{in: "openrouter/anthropic/claude-x", provider: "openrouter", model: "anthropic/claude-x"},
		{in: "anthropic/claude-or", errPart: `large model "anthropic/claude-or" not found`},
		{in: "shared", errPart: "found in multiple providers: "},
		{in: "off-model", errPart: `large model "off-model" not found`},
		{in: "nope/gpt-4o", errPart: `large model "nope/gpt-4o" not found`},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			large, _, err := findModels(characterizationProviders(), tt.in, "")
			require.NoError(t, err)
			got, err := validateMatches(large, tt.in, "large")
			if tt.errPart != "" {
				require.ErrorContains(t, err, tt.errPart)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.provider, got.provider)
			require.Equal(t, tt.model, got.modelID)
		})
	}
}

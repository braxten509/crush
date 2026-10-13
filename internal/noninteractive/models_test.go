package noninteractive

import (
	"testing"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/stretchr/testify/require"
)

func TestParseModelString(t *testing.T) {
	tests := []struct {
		name            string
		modelStr        string
		expectedFilter  string
		expectedModelID string
		setupProviders  func() map[string]config.ProviderConfig
	}{
		{
			name:            "simple model with no slashes",
			modelStr:        "gpt-4o",
			expectedFilter:  "",
			expectedModelID: "gpt-4o",
			setupProviders:  setupMockProviders,
		},
		{
			name:            "valid provider and model",
			modelStr:        "openai/gpt-4o",
			expectedFilter:  "openai",
			expectedModelID: "gpt-4o",
			setupProviders:  setupMockProviders,
		},
		{
			name:            "model with multiple slashes and first part is invalid provider",
			modelStr:        "moonshot/kimi-k2",
			expectedFilter:  "",
			expectedModelID: "moonshot/kimi-k2",
			setupProviders:  setupMockProviders,
		},
		{
			name:            "full path with valid provider and model with slashes",
			modelStr:        "synthetic/moonshot/kimi-k2",
			expectedFilter:  "synthetic",
			expectedModelID: "moonshot/kimi-k2",
			setupProviders:  setupMockProvidersWithSlashes,
		},
		{
			name:            "empty model string",
			modelStr:        "",
			expectedFilter:  "",
			expectedModelID: "",
			setupProviders:  setupMockProviders,
		},
		{
			name:            "model with trailing slash but valid provider",
			modelStr:        "openai/",
			expectedFilter:  "openai",
			expectedModelID: "",
			setupProviders:  setupMockProviders,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			providers := tt.setupProviders()
			filter, modelID := parseModelString(providers, tt.modelStr)

			require.Equal(t, tt.expectedFilter, filter, "provider filter mismatch")
			require.Equal(t, tt.expectedModelID, modelID, "model ID mismatch")
		})
	}
}

func setupMockProviders() map[string]config.ProviderConfig {
	return map[string]config.ProviderConfig{
		"openai": {
			ID:     "openai",
			Name:   "OpenAI",
			Models: []catwalk.Model{{ID: "gpt-4o"}, {ID: "gpt-4o-mini"}},
		},
		"anthropic": {
			ID:     "anthropic",
			Name:   "Anthropic",
			Models: []catwalk.Model{{ID: "claude-3-sonnet"}, {ID: "claude-3-opus"}},
		},
	}
}

func setupMockProvidersWithSlashes() map[string]config.ProviderConfig {
	return map[string]config.ProviderConfig{
		"synthetic": {
			ID:   "synthetic",
			Name: "Synthetic",
			Models: []catwalk.Model{
				{ID: "moonshot/kimi-k2"},
				{ID: "deepseek/deepseek-chat"},
			},
		},
		"openai": {
			ID:     "openai",
			Name:   "OpenAI",
			Models: []catwalk.Model{{ID: "gpt-4o"}},
		},
	}
}

func TestFindModels(t *testing.T) {
	tests := []struct {
		name             string
		modelStr         string
		expectedProvider string
		expectedModelID  string
		expectError      bool
		errorContains    string
		setupProviders   func() map[string]config.ProviderConfig
	}{
		{
			name:             "simple model found in one provider",
			modelStr:         "gpt-4o",
			expectedProvider: "openai",
			expectedModelID:  "gpt-4o",
			expectError:      false,
			setupProviders:   setupMockProviders,
		},
		{
			name:             "model with slashes in ID",
			modelStr:         "moonshot/kimi-k2",
			expectedProvider: "synthetic",
			expectedModelID:  "moonshot/kimi-k2",
			expectError:      false,
			setupProviders:   setupMockProvidersWithSlashes,
		},
		{
			name:             "provider and model with slashes in ID",
			modelStr:         "synthetic/moonshot/kimi-k2",
			expectedProvider: "synthetic",
			expectedModelID:  "moonshot/kimi-k2",
			expectError:      false,
			setupProviders:   setupMockProvidersWithSlashes,
		},
		{
			name:           "model not found",
			modelStr:       "nonexistent-model",
			expectError:    true,
			errorContains:  "not found",
			setupProviders: setupMockProviders,
		},
		{
			name:           "invalid provider specified",
			modelStr:       "nonexistent-provider/gpt-4o",
			expectError:    true,
			errorContains:  "provider",
			setupProviders: setupMockProviders,
		},
		{
			name:          "model found in multiple providers without provider filter",
			modelStr:      "shared-model",
			expectError:   true,
			errorContains: "multiple providers",
			setupProviders: func() map[string]config.ProviderConfig {
				return map[string]config.ProviderConfig{
					"openai": {
						ID:     "openai",
						Models: []catwalk.Model{{ID: "shared-model"}},
					},
					"anthropic": {
						ID:     "anthropic",
						Models: []catwalk.Model{{ID: "shared-model"}},
					},
				}
			},
		},
		{
			name:           "empty model string",
			modelStr:       "",
			expectError:    true,
			errorContains:  "not found",
			setupProviders: setupMockProviders,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			providers := tt.setupProviders()

			matches := findModels(providers, tt.modelStr)

			// Validate the matches.
			match, err := validateMatches(matches, tt.modelStr, "large")

			if tt.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errorContains)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.expectedProvider, match.Provider)
				require.Equal(t, tt.expectedModelID, match.ModelID)
			}
		})
	}
}

// characterizationProviders exercises the cases on which the two former
// copies of the matcher (in-process app and client/server cmd) disagreed.
func characterizationProviders() map[string]config.ProviderConfig {
	return map[string]config.ProviderConfig{
		"openai":     {ID: "openai", Models: []catwalk.Model{{ID: "gpt-4o"}, {ID: "shared"}}},
		"anthropic":  {ID: "anthropic", Models: []catwalk.Model{{ID: "claude-x"}, {ID: "shared"}}},
		"synthetic":  {ID: "synthetic", Models: []catwalk.Model{{ID: "moonshot/kimi-k2"}}},
		"openrouter": {ID: "openrouter", Models: []catwalk.Model{{ID: "anthropic/claude-x"}, {ID: "anthropic/claude-or"}}},
		"off":        {ID: "off", Disable: true, Models: []catwalk.Model{{ID: "off-model"}}},
	}
}

// TestResolveModel pins the unified semantics. Rows marked "changed" used to
// resolve differently in at least one of the two former copies.
func TestResolveModel(t *testing.T) {
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
		{in: "openai/GPT-4O", provider: "openai", model: "gpt-4o"},
		// Changed: the provider prefix is case-insensitive (was "not found"
		// in both copies).
		{in: "OpenAI/gpt-4o", provider: "openai", model: "gpt-4o"},
		// Changed for client/server mode, which split on the first slash
		// and reported "not found".
		{in: "moonshot/kimi-k2", provider: "synthetic", model: "moonshot/kimi-k2"},
		{in: "synthetic/moonshot/kimi-k2", provider: "synthetic", model: "moonshot/kimi-k2"},
		// A provider-qualified match wins over a full-ID match elsewhere.
		{in: "anthropic/claude-x", provider: "anthropic", model: "claude-x"},
		{in: "openrouter/anthropic/claude-x", provider: "openrouter", model: "anthropic/claude-x"},
		// Changed: the vendor prefix of an OpenRouter-style ID names a
		// provider that lacks the model, so the whole string is tried as an
		// ID (was "not found" in both copies).
		{in: "anthropic/claude-or", provider: "openrouter", model: "anthropic/claude-or"},
		{in: "shared", errPart: `large model: model "shared" found in multiple providers: `},
		{in: "off-model", errPart: `large model "off-model" not found`},
		{in: "off/off-model", errPart: `large model "off/off-model" not found`},
		{in: "nope/gpt-4o", errPart: `large model "nope/gpt-4o" not found`},
		{in: "openai/", errPart: `large model "openai/" not found`},
		{in: "", errPart: `large model "" not found`},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			got, err := ResolveModel(characterizationProviders(), tt.in, "large")
			if tt.errPart != "" {
				require.ErrorContains(t, err, tt.errPart)
				return
			}
			require.NoError(t, err)
			require.Equal(t, ModelMatch{Provider: tt.provider, ModelID: tt.model}, got)
		})
	}
}

func TestResolveModel_AmbiguousNamesEveryProvider(t *testing.T) {
	t.Parallel()

	_, err := ResolveModel(characterizationProviders(), "shared", "small")
	require.ErrorContains(t, err, "small model: ")
	require.ErrorContains(t, err, "anthropic")
	require.ErrorContains(t, err, "openai")
	require.ErrorContains(t, err, "Please specify provider using 'provider/model' format")
}

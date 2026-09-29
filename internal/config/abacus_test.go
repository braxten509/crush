package config

import (
	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/csync"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestAbacusControlsSurviveModelResolution(t *testing.T) {
	model := catwalk.Model{ID: "gpt-6.1-sol", DefaultMaxTokens: 128000, CanReason: true, ReasoningLevels: []string{"low", "medium", "high", "xhigh", "max"}, DefaultReasoningEffort: "medium"}
	cfg := &Config{
		Options:   &Options{DisableDefaultProviders: true},
		Providers: csync.NewMapFrom(map[string]ProviderConfig{"abacus": {ID: "abacus", APIKey: "test-key", Models: []catwalk.Model{model}}}),
		Models: map[SelectedModelType]SelectedModel{
			SelectedModelTypeLarge: {Provider: "abacus", Model: model.ID, ServiceTier: "fast", ReasoningEffort: "max", MaxTokens: 64000},
			SelectedModelTypeSmall: {Provider: "abacus", Model: model.ID, ServiceTier: "default", ReasoningEffort: "low", MaxTokens: 8192},
		},
	}
	resolved, err := resolveSelectedModels(cfg, nil)
	require.NoError(t, err)
	require.Equal(t, "fast", resolved.Large.ServiceTier)
	require.Equal(t, "default", resolved.Small.ServiceTier)
	require.Equal(t, "max", resolved.Large.ReasoningEffort)
	require.Equal(t, int64(64000), resolved.Large.MaxTokens)
}

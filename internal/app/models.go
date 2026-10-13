package app

import (
	"log/slog"
	"strings"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/config"
)

// GetDefaultSmallModel returns the default small model for the given
// provider. Falls back to the large model if no default is found.
func (app *App) GetDefaultSmallModel(providerID string) config.SelectedModel {
	cfg := app.config.Config()
	largeModelCfg := cfg.Models[config.SelectedModelTypeLarge]
	if small, ok := cfg.CLISmallModel(providerID); ok {
		return small
	}

	// Find the provider in the known providers list to get its default small model.
	knownProviders, _ := config.Providers(cfg)
	var knownProvider *catwalk.Provider
	for _, p := range knownProviders {
		if string(p.ID) == providerID {
			knownProvider = &p
			break
		}
	}

	// For unknown/local providers, use the large model as small.
	if knownProvider == nil {
		slog.Warn("Using large model as small model for unknown provider", "provider", providerID, "model", largeModelCfg.Model)
		return largeModelCfg
	}

	defaultSmallModelID := knownProvider.DefaultSmallModelID
	model := cfg.GetModel(providerID, defaultSmallModelID)
	if model == nil {
		slog.Warn("Default small model not found, using large model", "provider", providerID, "model", largeModelCfg.Model)
		return largeModelCfg
	}

	// A subscription-authenticated provider only serves the models its
	// subscription grants, so the default small model must come from that
	// catalog as well. This applies to ChatGPT-authenticated OpenAI and
	// Grok-authenticated xAI alike.
	if largeModelCfg.Provider == providerID {
		var pick func(config.ProviderConfig) *catwalk.Model
		switch providerID {
		case string(catwalk.InferenceProviderOpenAI):
			pick = chatGPTSmallModel
		case string(catwalk.InferenceProviderXAI):
			pick = grokSmallModel
		}
		if pick != nil {
			if pc, ok := cfg.Providers.Get(providerID); ok && pc.OAuthToken != nil {
				if small := pick(pc); small != nil {
					return config.SelectedModel{
						Provider:        providerID,
						Model:           small.ID,
						MaxTokens:       small.DefaultMaxTokens,
						ReasoningEffort: small.DefaultReasoningEffort,
					}
				}
				return largeModelCfg
			}
		}
	}

	slog.Info("Using provider default small model", "provider", providerID, "model", defaultSmallModelID)
	return config.SelectedModel{
		Provider:        providerID,
		Model:           defaultSmallModelID,
		MaxTokens:       model.DefaultMaxTokens,
		ReasoningEffort: model.DefaultReasoningEffort,
	}
}

// chatGPTSmallModel picks a lightweight model from the ChatGPT catalog,
// preferring a "mini" variant and falling back to the last entry (the
// catalog lists heavier models first). Returns nil when the catalog is
// empty.
func chatGPTSmallModel(pc config.ProviderConfig) *catwalk.Model {
	for i := range pc.ChatGPTModels {
		if strings.Contains(pc.ChatGPTModels[i].ID, "mini") {
			return &pc.ChatGPTModels[i]
		}
	}
	if len(pc.ChatGPTModels) > 0 {
		return &pc.ChatGPTModels[len(pc.ChatGPTModels)-1]
	}
	return nil
}

// grokSmallModel picks a lightweight model from the Grok catalog,
// preferring a "fast" or "mini" variant and falling back to the last
// entry (the catalog lists heavier models first). Returns nil when the
// catalog is empty.
func grokSmallModel(pc config.ProviderConfig) *catwalk.Model {
	for _, marker := range []string{"fast", "mini"} {
		for i := range pc.GrokModels {
			if strings.Contains(pc.GrokModels[i].ID, marker) {
				return &pc.GrokModels[i]
			}
		}
	}
	if len(pc.GrokModels) > 0 {
		return &pc.GrokModels[len(pc.GrokModels)-1]
	}
	return nil
}

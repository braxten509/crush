package config

import (
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"

	"charm.land/catwalk/pkg/catwalk"
)

func (c *Config) defaultModelSelection(knownProviders []catwalk.Provider) (largeModel SelectedModel, smallModel SelectedModel, err error) {
	if len(knownProviders) == 0 && c.Providers.Len() == 0 {
		err = fmt.Errorf("no providers configured, please configure at least one provider")
		return largeModel, smallModel, err
	}

	// Agent CLIs come first: they need no API key and are this fork's point.
	if large, small, ok := c.defaultCLIModels(); ok {
		return large, small, nil
	}

	// Use the first provider enabled based on the known providers order
	// if no provider found that is known use the first provider configured
	for _, p := range knownProviders {
		providerConfig, ok := c.Providers.Get(string(p.ID))
		if !ok || providerConfig.Disable {
			continue
		}
		defaultLargeModel := c.GetModel(string(p.ID), p.DefaultLargeModelID)
		if defaultLargeModel == nil {
			slog.Warn("Default large model %s not found for provider %s", p.DefaultLargeModelID, p.ID)
			if len(providerConfig.Models) == 0 {
				return largeModel, smallModel, fmt.Errorf("default large model %s not found for provider %s", p.DefaultLargeModelID, p.ID)
			}
			defaultLargeModel = &providerConfig.Models[0]
		}
		largeModel = SelectedModel{
			Provider:        string(p.ID),
			Model:           defaultLargeModel.ID,
			MaxTokens:       defaultLargeModel.DefaultMaxTokens,
			ReasoningEffort: defaultLargeModel.DefaultReasoningEffort,
		}

		defaultSmallModel := c.GetModel(string(p.ID), p.DefaultSmallModelID)
		if defaultSmallModel == nil {
			slog.Warn("Default small model %s not found for provider %s", p.DefaultSmallModelID, p.ID)
			if len(providerConfig.Models) == 0 {
				return largeModel, smallModel, fmt.Errorf("default small model %s not found for provider %s", p.DefaultSmallModelID, p.ID)
			}
			defaultSmallModel = &providerConfig.Models[0]
		}
		smallModel = SelectedModel{
			Provider:        string(p.ID),
			Model:           defaultSmallModel.ID,
			MaxTokens:       defaultSmallModel.DefaultMaxTokens,
			ReasoningEffort: defaultSmallModel.DefaultReasoningEffort,
		}
		return largeModel, smallModel, err
	}

	enabledProviders := c.EnabledProviders()
	slices.SortFunc(enabledProviders, func(a, b ProviderConfig) int {
		return strings.Compare(a.ID, b.ID)
	})

	if len(enabledProviders) == 0 {
		err = fmt.Errorf("no providers configured, please configure at least one provider")
		return largeModel, smallModel, err
	}

	providerConfig := enabledProviders[0]
	if len(providerConfig.Models) == 0 {
		err = fmt.Errorf("provider %s has no models configured", providerConfig.ID)
		return largeModel, smallModel, err
	}
	defaultLargeModel := c.GetModel(providerConfig.ID, providerConfig.Models[0].ID)
	largeModel = SelectedModel{
		Provider:  providerConfig.ID,
		Model:     defaultLargeModel.ID,
		MaxTokens: defaultLargeModel.DefaultMaxTokens,
	}
	defaultSmallModel := c.GetModel(providerConfig.ID, providerConfig.Models[0].ID)
	smallModel = SelectedModel{
		Provider:  providerConfig.ID,
		Model:     defaultSmallModel.ID,
		MaxTokens: defaultSmallModel.DefaultMaxTokens,
	}
	return largeModel, smallModel, err
}

// resolvedModels holds the result of resolving user-configured model
// selections against the provider catalog.
type resolvedModels struct {
	Large         SelectedModel
	Small         SelectedModel
	LargeFallback bool // true if Large was corrected to a default
	SmallFallback bool // true if Small was corrected to a default
}

// resolveSelectedModels validates the user's configured model selections
// against the provider catalog, falling back to defaults when a model ID is
// invalid. It is pure resolution logic: it does not mutate the store or
// touch disk. The caller assigns the results to c.Models and persists any
// fallback corrections as appropriate.
func resolveSelectedModels(cfg *Config, knownProviders []catwalk.Provider) (resolvedModels, error) {
	var result resolvedModels
	defaultLarge, defaultSmall, err := cfg.defaultModelSelection(knownProviders)
	if err != nil {
		return result, fmt.Errorf("failed to select default models: %w", err)
	}
	large, small := defaultLarge, defaultSmall

	largeModelSelected, largeModelConfigured := cfg.Models[SelectedModelTypeLarge]
	if largeModelConfigured {
		large, result.LargeFallback = cfg.resolveSelectedModel(defaultLarge, largeModelSelected)
	}
	smallModelSelected, smallModelConfigured := cfg.Models[SelectedModelTypeSmall]
	if smallModelConfigured {
		small, result.SmallFallback = cfg.resolveSelectedModel(defaultSmall, smallModelSelected)
	}

	// When small isn't explicitly configured and the provider isn't a
	// known built-in, use the large model as the small model. This
	// prevents two different models from being requested concurrently
	// for local/openai-compat providers.
	if !smallModelConfigured {
		isKnownProvider := false
		for _, kp := range knownProviders {
			if string(kp.ID) == small.Provider {
				isKnownProvider = true
				break
			}
		}
		if cliSmall, ok := cfg.CLISmallModel(large.Provider); ok {
			small = cliSmall
		} else if !isKnownProvider {
			slog.Warn("Using large model as small model for unknown provider", "provider", large.Provider, "model", large.Model)
			small = large
		}
	}

	result.Large = large
	result.Small = small
	return result, nil
}

// resolveSelectedModel lays a configured model selection over the default
// one. When the selected model does not exist it returns the default and
// reports the fallback. Otherwise unset limits and reasoning effort come
// from the model's defaults, and the sampling settings come from the
// selection.
func (c *Config) resolveSelectedModel(def, selected SelectedModel) (SelectedModel, bool) {
	resolved := def
	if selected.Model != "" {
		resolved.Model = selected.Model
	}
	if selected.Provider != "" {
		resolved.Provider = selected.Provider
	}
	model := c.GetModel(resolved.Provider, resolved.Model)
	if model == nil {
		return def, true
	}
	resolved.ServiceTier = selected.ServiceTier
	resolved.Ultracode = selected.Ultracode
	if selected.MaxTokens > 0 {
		resolved.MaxTokens = selected.MaxTokens
	} else {
		resolved.MaxTokens = model.DefaultMaxTokens
	}
	if selected.ReasoningEffort != "" {
		resolved.ReasoningEffort = selected.ReasoningEffort
	} else {
		resolved.ReasoningEffort = model.DefaultReasoningEffort
	}
	resolved.Think = selected.Think
	if selected.Temperature != nil {
		resolved.Temperature = selected.Temperature
	}
	if selected.TopP != nil {
		resolved.TopP = selected.TopP
	}
	if selected.TopK != nil {
		resolved.TopK = selected.TopK
	}
	if selected.FrequencyPenalty != nil {
		resolved.FrequencyPenalty = selected.FrequencyPenalty
	}
	if selected.PresencePenalty != nil {
		resolved.PresencePenalty = selected.PresencePenalty
	}
	if selected.ProviderOptions != nil {
		resolved.ProviderOptions = maps.Clone(selected.ProviderOptions)
	}
	return resolved, false
}

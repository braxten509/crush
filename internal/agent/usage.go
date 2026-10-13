package agent

import (
	"log/slog"
	"strconv"

	"charm.land/fantasy"
	"charm.land/fantasy/providers/openai"
	"charm.land/fantasy/providers/openrouter"

	"github.com/charmbracelet/crush/internal/agent/hyper"
	"github.com/charmbracelet/crush/internal/session"
)

func (a *sessionAgent) openrouterCost(metadata fantasy.ProviderMetadata) *float64 {
	openrouterMetadata, ok := metadata[openrouter.Name]
	if !ok {
		return nil
	}

	opts, ok := openrouterMetadata.(*openrouter.ProviderMetadata)
	if !ok {
		return nil
	}
	return &opts.Usage.Cost
}

// extractPrismModel returns the ID and name of the model that actually
// served the turn, as reported by the Hyper Prism model router headers,
// or empty strings when the turn was not routed through a Prism model.
func extractPrismModel(metadata fantasy.ProviderMetadata) (modelID, modelName string) {
	openaiMeta, ok := metadata[openai.Name]
	if !ok {
		return "", ""
	}
	pm, ok := openaiMeta.(*openai.ProviderMetadata)
	if !ok {
		return "", ""
	}
	_ = pm.ExtraField(hyper.PrismModelIDField, &modelID)
	_ = pm.ExtraField(hyper.PrismModelNameField, &modelName)
	return modelID, modelName
}

// extractPrismSavings returns the hypercredit and dollar savings from
// routing the turn through the Hyper Prism model router, as reported by
// its savings trailers, or nil when not reported or malformed.
func extractPrismSavings(metadata fantasy.ProviderMetadata) (hypercredits, dollars *float64) {
	openaiMeta, ok := metadata[openai.Name]
	if !ok {
		return nil, nil
	}
	pm, ok := openaiMeta.(*openai.ProviderMetadata)
	if !ok {
		return nil, nil
	}
	return extraFieldFloat(pm, hyper.PrismHypercreditSavingsField), extraFieldFloat(pm, hyper.PrismDollarSavingsField)
}

func extraFieldFloat(pm *openai.ProviderMetadata, key string) *float64 {
	var value string
	if !pm.ExtraField(key, &value) {
		return nil
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		slog.Warn("Could not parse Prism savings", "key", key, "value", value, "error", err)
		return nil
	}
	return &parsed
}

func (a *sessionAgent) updateSessionUsage(model Model, session *session.Session, usage fantasy.Usage, overrideCost *float64, estimated bool) {
	if !usageIsZero(usage) {
		session.EstimatedUsage = estimated
	}

	modelConfig := model.CatwalkCfg
	cost := modelConfig.CostPer1MInCached/1e6*float64(usage.CacheCreationTokens) +
		modelConfig.CostPer1MOutCached/1e6*float64(usage.CacheReadTokens) +
		modelConfig.CostPer1MIn/1e6*float64(usage.InputTokens) +
		modelConfig.CostPer1MOut/1e6*float64(usage.OutputTokens)

	if !estimated {
		a.eventTokensUsed(session.ID, model, usage, cost)
	}

	if estimated {
		cost = 0
	} else {
		// Use override cost if available (e.g., from OpenRouter).
		if overrideCost != nil {
			cost = *overrideCost
		}

		// Skip cost accumulation
		if model.FlatRate {
			cost = 0
		}
	}

	session.Cost += cost
	updateSessionTokenCounters(session, usage)
}

// contextTokens returns the size of the prompt the provider processed for
// a step. Providers with prompt caching (Anthropic, Bedrock, Vercel) report
// the prompt as three disjoint buckets: tokens served from cache, tokens
// newly written to the cache, and the uncached remainder. All three occupy
// the context window, so all three count. Providers without cache writes
// leave CacheCreationTokens at zero, and the OpenAI-style providers already
// subtract cached tokens from InputTokens, so nothing is counted twice.
func contextTokens(usage fantasy.Usage) int64 {
	return usage.InputTokens + usage.CacheReadTokens + usage.CacheCreationTokens
}

func updateSessionTokenCounters(session *session.Session, usage fantasy.Usage) {
	if usage.OutputTokens != 0 {
		session.CompletionTokens = usage.OutputTokens
	}
	if promptTokens := contextTokens(usage); promptTokens != 0 {
		session.PromptTokens = promptTokens
	}
}

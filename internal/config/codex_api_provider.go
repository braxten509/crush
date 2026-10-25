package config

import (
	"slices"
	"strings"
	"testing"

	"charm.land/catwalk/pkg/catwalk"
)

// CodexAPIProviderID is OpenAI's API billed per use, shown as "Codex API"
// (owner, 2026-10-25). It is always listed, key or not, so Settings →
// Accounts can add the key; the config only needs `api_key`.
const CodexAPIProviderID = "codex-api"

// listCodexAPI is off in tests, so their provider lists hold only what they
// set up.
var listCodexAPI = !testing.Testing()

// codexAPIUltrafast marks a model entry that is the real model on OpenAI's
// ultrafast service tier.
const codexAPIUltrafast = "-ultrafast"

// codexAPIModels are OpenAI's models with their per-use prices for requests
// of up to 272K input tokens (developers.openai.com/api/docs/pricing,
// 2026-10-25; cache writes at the 30-minute rate). An ultrafast entry costs
// 6x. Longer requests cost more: see PriceForRequest.
func codexAPIModels() []catwalk.Model {
	levels := []string{"low", "medium", "high", "xhigh", "max"}
	model := func(id, name string, in, out, cached, write float64) catwalk.Model {
		return catwalk.Model{
			ID: id, Name: name,
			CostPer1MIn: in, CostPer1MOut: out, CostPer1MOutCached: cached, CostPer1MInCached: write,
			ContextWindow: 1050000, DefaultMaxTokens: 128000,
			CanReason: true, ReasoningLevels: levels, DefaultReasoningEffort: "medium",
			SupportsImages: true,
		}
	}
	return []catwalk.Model{
		model("gpt-6.1-sol", "GPT-6.1 Sol", 2, 10, 0.1, 2.5),
		model("gpt-6.1-sol"+codexAPIUltrafast, "GPT-6.1 Sol Ultrafast", 12, 60, 0.6, 15),
		model("gpt-6-astra", "GPT-6 Astra", 10, 50, 1, 12.5),
		model("gpt-6-astra"+codexAPIUltrafast, "GPT-6 Astra Ultrafast", 60, 300, 6, 75),
	}
}

// addCodexAPIProvider lists the Codex API provider, keeping the saved key,
// name and other settings, and always giving it the current models. A
// disabled entry is left alone.
func (c *Config) addCodexAPIProvider() {
	cfg, _ := c.Providers.Get(CodexAPIProviderID)
	if cfg.Disable {
		return
	}
	cfg.ID = CodexAPIProviderID
	if cfg.Name == "" {
		cfg.Name = "Codex API"
	}
	cfg.Type = catwalk.TypeOpenAI
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.openai.com/v1"
	}
	off := false
	cfg.AutoDiscoverModels = &off
	cfg.Models = codexAPIModels()
	c.Providers.Set(CodexAPIProviderID, cfg)
}

// IsCodexAPIModel reports whether id is one of the Codex API's models,
// ultrafast entries included.
func IsCodexAPIModel(id string) bool {
	return slices.ContainsFunc(codexAPIModels(), func(m catwalk.Model) bool { return m.ID == id })
}

// CodexAPIRequestModel is the model and service tier a request for the
// Codex API model id sends: an ultrafast entry is its real model on the
// "ultrafast" tier, any other id goes as it is with no tier.
func CodexAPIRequestModel(id string) (model, tier string) {
	if real, ok := strings.CutSuffix(id, codexAPIUltrafast); ok {
		return real, "ultrafast"
	}
	return id, ""
}

// codexAPILongContext is the input size past which OpenAI bills the whole
// request at its long-context rates: input, cached input and cache writes
// 2x, output 1.5x (developers.openai.com/api/docs/pricing, 2026-10-25).
const codexAPILongContext = 272000

// PriceForRequest is m with the prices one request of inputTokens (fresh,
// cached and written to the cache together) is billed at. Only the Codex
// API's models have a long-context tier; every other model is unchanged.
func PriceForRequest(m catwalk.Model, inputTokens int64) catwalk.Model {
	if inputTokens <= codexAPILongContext || !IsCodexAPIModel(m.ID) {
		return m
	}
	m.CostPer1MIn *= 2
	m.CostPer1MInCached *= 2
	m.CostPer1MOutCached *= 2
	m.CostPer1MOut *= 1.5
	return m
}

// codexAPICompactAt is where a Codex API chat compacts while the price-step
// switch is on: below codexAPILongContext, with room for the turn's next
// request (the new message and tool results) to stay under it.
const codexAPICompactAt = 240000

// PriceStepCompactionLimit is limit, lowered for the Codex API so its chats
// compact before OpenAI bills a request at the long-context price, unless
// options.codex_api_compact_before_price_step is false.
func (o *Options) PriceStepCompactionLimit(providerID string, limit int64) int64 {
	if providerID != CodexAPIProviderID || o != nil && o.CodexAPICompactBeforePriceStep != nil && !*o.CodexAPICompactBeforePriceStep {
		return limit
	}
	return min(limit, codexAPICompactAt)
}

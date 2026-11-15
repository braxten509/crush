package config

import (
	"net/url"
	"path/filepath"
	"slices"
	"strings"

	"charm.land/catwalk/v3/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/discover"
)

// Every API model's price comes from a live source when one exists, and
// the shipped or hardcoded price only when none does (owner, 2026-11-15).
// Which live price applies depends on who sells the model:
//
//   - A provider whose own API reports prices (OpenRouter, OrcaRouter,
//     Tinfoil, Abacus, LiteLLM, Kimi ...) already carries them: that is
//     what it bills, so it is left alone here.
//   - A model's maker selling it on its own API (Anthropic, Google,
//     OpenAI, xAI, DeepSeek ...) takes the price of the maker's own
//     endpoint in OpenRouter's public endpoint list for the model (see
//     discover/prices.go).
//   - Any other host keeps its own price: a host's rates are its own, so
//     it never takes a maker's price that merely shares the model's name.
//
// A provider is a maker by the host it talks to, not by its name, so a
// custom provider pointed at a maker's API is covered, and a provider
// named "openai" pointed at a reseller is not.

// makerHosts maps a maker's own API host to the vendor OpenRouter files its
// models under. Moonshot's .cn and MiniMax's China API bill in yuan and
// Z.ai's listed endpoint is its coding plan, so they are not here.
var makerHosts = map[string]string{
	"api.anthropic.com":                 "anthropic",
	"api.openai.com":                    "openai",
	"generativelanguage.googleapis.com": "google",
	"api.x.ai":                          "x-ai",
	"api.deepseek.com":                  "deepseek",
	"api.moonshot.ai":                   "moonshotai",
	"api.mistral.ai":                    "mistralai",
	"api.minimax.io":                    "minimax",
}

// makerDefaultEndpoint names the catalog providers whose endpoint is an
// environment variable that is normally unset: their SDK then talks to the
// maker's own API.
var makerDefaultEndpoint = map[string]string{
	string(catwalk.InferenceProviderAnthropic): "anthropic",
	string(catwalk.InferenceProviderOpenAI):    "openai",
	string(catwalk.InferenceProviderGemini):    "google",
}

// codexAPIUltrafastPrice is how many times its model's price an ultrafast
// Codex API entry costs (see codexAPIModels).
const codexAPIUltrafastPrice = 6

// livePricesPath is where the last read of the live prices is saved, next
// to the discovery cache dir; empty when that cache is off.
func livePricesPath(discoveryDir string) string {
	if discoveryDir == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(discoveryDir), "live_prices.json")
}

// applyLivePrices lays the live prices over every maker provider's models.
// It runs once the whole provider list is built, so every provider, a
// future one included, goes through it.
func (s *providerSetup) applyLivePrices() {
	prices := discover.LivePrices(livePricesPath(s.discoveryDir))
	if len(prices) == 0 {
		return
	}
	for id, providerConfig := range s.cfg.Providers.Seq2() {
		models, changed := livePricedModels(id, providerConfig, s.resolver, prices)
		if !changed {
			continue
		}
		providerConfig.Models = models
		s.cfg.Providers.Set(id, providerConfig)
	}
}

// livePricedModels is the provider's models with each one the maker's
// live price is known for priced at it, and whether any price changed. The
// live price replaces the old one whole: a cache rate it leaves out is one
// the maker does not charge, and mixing in the shipped one would bill a
// charge that is not there.
func livePricedModels(id string, pc ProviderConfig, resolver VariableResolver, prices map[string]catwalk.Pricing) ([]catwalk.Model, bool) {
	if len(prices) == 0 {
		return pc.Models, false
	}
	vendor := makerOf(id, pc, resolver)
	if vendor == "" {
		return pc.Models, false
	}
	var models []catwalk.Model
	for i, model := range pc.Models {
		lookup, factor := livePriceModel(id, model.ID)
		live, ok := prices[discover.LivePriceKey(vendor, lookup)]
		if !ok {
			continue
		}
		priced := catwalk.Pricing{
			Input:       live.Input * factor,
			Output:      live.Output * factor,
			CacheCreate: live.CacheCreate * factor,
			CacheHit:    live.CacheHit * factor,
		}
		if priced == model.Pricing {
			continue
		}
		if models == nil {
			models = slices.Clone(pc.Models)
		}
		models[i].Pricing = priced
	}
	if models == nil {
		return pc.Models, false
	}
	return models, true
}

// livePriceModel is the model whose live price a provider's model id
// takes, and how many times that price it costs. An ultrafast Codex API
// entry is its real model on a dearer tier.
func livePriceModel(providerID, modelID string) (string, float64) {
	if providerID == CodexAPIProviderID {
		if real, tier := CodexAPIRequestModel(modelID); tier != "" {
			return real, codexAPIUltrafastPrice
		}
	}
	return modelID, 1
}

// livePriceWants lists every model of a maker provider in c, so the
// background refresh knows whose prices to look up.
func (c *Config) livePriceWants(resolver VariableResolver) []discover.LivePriceWant {
	var wants []discover.LivePriceWant
	for id, pc := range c.Providers.Seq2() {
		vendor := makerOf(id, pc, resolver)
		if vendor == "" {
			continue
		}
		for _, model := range pc.Models {
			lookup, _ := livePriceModel(id, model.ID)
			wants = append(wants, discover.LivePriceWant{Vendor: vendor, ID: lookup})
		}
	}
	return wants
}

// makerOf is the OpenRouter vendor of the maker whose own API the provider
// talks to, billed per use, or empty for any other provider.
func makerOf(id string, pc ProviderConfig, resolver VariableResolver) string {
	if pc.Disable || pc.FlatRate || IsCLIProviderType(pc.Type) {
		return ""
	}
	return providerMaker(id, pc, resolver)
}

// providerMaker is the OpenRouter vendor of the maker whose own API the
// provider talks to, or empty when it talks to any other host.
func providerMaker(id string, pc ProviderConfig, resolver VariableResolver) string {
	baseURL, err := resolver.ResolveValue(pc.BaseURL)
	if err != nil || strings.TrimSpace(baseURL) == "" {
		return makerDefaultEndpoint[id]
	}
	u, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return ""
	}
	return makerHosts[strings.ToLower(u.Hostname())]
}

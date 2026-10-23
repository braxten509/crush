package discover

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"charm.land/catwalk/pkg/catwalk"
)

// openRouterMaxTokens caps a saved OpenRouter model's reply length, as the
// other configured API models do.
const openRouterMaxTokens = 32_000

// openRouterEfforts are OpenRouter's reasoning.effort levels, lowest first,
// without "none" (reasoning off): a model whose catalog entry lists no
// supported_efforts takes them all.
var openRouterEfforts = []string{"minimal", "low", "medium", "high", "xhigh", "max"}

type openRouterModel struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	ContextLength int64  `json:"context_length"`
	Architecture  struct {
		InputModalities []string `json:"input_modalities"`
	} `json:"architecture"`
	Pricing struct {
		Prompt          string `json:"prompt"`
		Completion      string `json:"completion"`
		InputCacheRead  string `json:"input_cache_read"`
		InputCacheWrite string `json:"input_cache_write"`
	} `json:"pricing"`
	TopProvider struct {
		MaxCompletionTokens int64 `json:"max_completion_tokens"`
	} `json:"top_provider"`
	SupportedParameters []string `json:"supported_parameters"`
	// Reasoning is the model's effort choice; nil when it offers none.
	Reasoning *struct {
		SupportedEfforts []string `json:"supported_efforts"`
		DefaultEffort    string   `json:"default_effort"`
	} `json:"reasoning"`
}

// OpenRouterCatalog lists the models the OpenRouter account may use: its
// /models/user list, which applies the account's provider, privacy and
// guardrail preferences. Models that cannot call tools are left out, since
// Crush's agents need them. The models come back as config entries, ready
// to be saved under the provider.
func OpenRouterCatalog(ctx context.Context, baseURL, apiKey string, headers map[string]string, resolver Resolver) ([]catwalk.Model, error) {
	resp, err := doRequest(ctx, http.MethodGet, baseURL, "/models/user", apiKey, headers, resolver, nil)
	if err != nil {
		return nil, fmt.Errorf("reading the OpenRouter catalog: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("reading the OpenRouter catalog: %s: %s", resp.Status, body)
	}
	var list struct {
		Data []openRouterModel `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, fmt.Errorf("reading the OpenRouter catalog: %w", err)
	}
	models := make([]catwalk.Model, 0, len(list.Data))
	for _, entry := range list.Data {
		if entry.ID == "" || !slices.Contains(entry.SupportedParameters, "tools") {
			continue
		}
		models = append(models, openRouterCatwalkModel(entry))
	}
	return models, nil
}

// OpenRouterModelName drops the vendor word OpenRouter puts before a
// model's name ("DeepSeek: DeepSeek V4.1 Flash 1017"), which reads badly
// beside the provider's own name in the model lists (owner, 2026-10-22). A
// name whose first words are longer than a vendor's, or that has no such
// prefix, comes back as it is.
func OpenRouterModelName(name string) string {
	prefix, rest, ok := strings.Cut(name, ": ")
	if !ok || strings.TrimSpace(rest) == "" || len([]rune(prefix)) > 24 || len(strings.Fields(prefix)) > 3 {
		return name
	}
	return rest
}

func openRouterCatwalkModel(entry openRouterModel) catwalk.Model {
	maxTokens := entry.TopProvider.MaxCompletionTokens
	if maxTokens <= 0 || maxTokens > openRouterMaxTokens {
		maxTokens = openRouterMaxTokens
	}
	model := catwalk.Model{
		ID:                 entry.ID,
		Name:               OpenRouterModelName(entry.Name),
		ContextWindow:      entry.ContextLength,
		DefaultMaxTokens:   maxTokens,
		SupportsImages:     slices.Contains(entry.Architecture.InputModalities, "image"),
		CostPer1MIn:        perMillion(entry.Pricing.Prompt),
		CostPer1MOut:       perMillion(entry.Pricing.Completion),
		CostPer1MInCached:  perMillion(entry.Pricing.InputCacheRead),
		CostPer1MOutCached: perMillion(entry.Pricing.InputCacheWrite),
	}
	if model.Name == "" {
		model.Name = entry.ID
	}
	if slices.Contains(entry.SupportedParameters, "reasoning") {
		model.CanReason = true
		model.ReasoningLevels, model.DefaultReasoningEffort = openRouterLevels(entry)
	}
	return model
}

// openRouterLevels are the effort levels a model's catalog entry lists
// (owner, 2026-10-22), lowest first, and its default: the entry's own, else
// the lowest level (a default of "none" is reasoning off). No reasoning
// object means the model takes no effort.
func openRouterLevels(entry openRouterModel) ([]string, string) {
	if entry.Reasoning == nil {
		return nil, ""
	}
	levels := slices.Clone(openRouterEfforts)
	if entry.Reasoning.SupportedEfforts != nil {
		levels = slices.DeleteFunc(levels, func(l string) bool { return !slices.Contains(entry.Reasoning.SupportedEfforts, l) })
	}
	if len(levels) == 0 {
		return nil, ""
	}
	if slices.Contains(levels, entry.Reasoning.DefaultEffort) {
		return levels, entry.Reasoning.DefaultEffort
	}
	return levels, levels[0]
}

// perMillion turns OpenRouter's price per token into the price per million.
// A router's "-1" (the price depends on the model it picks) is no price.
func perMillion(perToken string) float64 {
	price, err := strconv.ParseFloat(perToken, 64)
	if err != nil || price <= 0 {
		return 0
	}
	return price * 1_000_000
}

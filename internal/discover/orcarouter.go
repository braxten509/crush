package discover

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"

	"charm.land/catwalk/pkg/catwalk"
)

// orcaRouterEfforts are the effort levels every OrcaRouter model that
// thinks gets (owner, 2026-10-27): its list names none per model.
var orcaRouterEfforts = []string{"low", "medium", "high"}

type orcaRouterModel struct {
	openRouterModel
	// MaxCompletionTokens is OrcaRouter's own reply cap, beside the
	// OpenRouter-style top_provider one.
	MaxCompletionTokens int64 `json:"max_completion_tokens"`
	// EndpointTypes and EndpointCapabilities say which APIs serve the
	// model; many chat models list no parameters at all.
	EndpointTypes        []string `json:"supported_endpoint_types"`
	EndpointCapabilities []string `json:"endpoint_capabilities"`
}

// orcaRouterChat reports whether a model takes chat requests: one that
// lists tools, one whose capabilities name chat_completions, or, with no
// capabilities listed (OrcaRouter's own routers), one served over the
// OpenAI API. Voice models also use the OpenAI API but list audio_speech.
func orcaRouterChat(entry orcaRouterModel) bool {
	if slices.Contains(entry.SupportedParameters, "tools") {
		return true
	}
	if entry.EndpointCapabilities != nil {
		return slices.Contains(entry.EndpointCapabilities, "chat_completions")
	}
	return slices.Contains(entry.EndpointTypes, "openai")
}

// orcaRouterPrice is one entry of OrcaRouter's public /api/pricing list.
// Prices there are ratios: one ratio point is $2 a million input tokens,
// output and cached input are the input price times their own ratio. A
// quota type other than 0 is priced per request, not per token.
type orcaRouterPrice struct {
	ModelName           string   `json:"model_name"`
	DisplayName         string   `json:"display_name"`
	QuotaType           int      `json:"quota_type"`
	ModelRatio          float64  `json:"model_ratio"`
	CompletionRatio     float64  `json:"completion_ratio"`
	CacheRatio          float64  `json:"cache_ratio"`
	ContextLength       int64    `json:"context_length"`
	MaxCompletionTokens int64    `json:"max_completion_tokens"`
	InputModalities     []string `json:"input_modalities"`
	SupportedParameters []string `json:"supported_parameters"`
}

// OrcaRouterCatalog lists OrcaRouter's models from its /models list, which
// is shaped like OpenRouter's. Every chat model is kept, OrcaRouter's own
// auto-pick routers too (owner, 2026-10-28); picture, video, voice and
// search-index models are left out. A model with no size listed gets
// 200K. Models that call tools but only appear on the public price list
// are added after them: /models hides the locked OrcaCyber models, which
// need an approved security account (owner, 2026-10-28). The models come
// back as config entries, ready to be saved under the provider.
func OrcaRouterCatalog(ctx context.Context, baseURL, apiKey string, headers map[string]string, resolver Resolver) ([]catwalk.Model, error) {
	resp, err := doRequest(ctx, http.MethodGet, baseURL, "/models", apiKey, headers, resolver, nil)
	if err != nil {
		return nil, fmt.Errorf("reading the OrcaRouter catalog: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("reading the OrcaRouter catalog: %s: %s", resp.Status, body)
	}
	var list struct {
		Data []orcaRouterModel `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, fmt.Errorf("reading the OrcaRouter catalog: %w", err)
	}
	models := make([]catwalk.Model, 0, len(list.Data))
	listed := make(map[string]bool, len(list.Data))
	for _, entry := range list.Data {
		if entry.ID == "" || !orcaRouterChat(entry) {
			continue
		}
		listed[entry.ID] = true
		models = append(models, orcaRouterCatwalkModel(entry))
	}
	for _, price := range orcaRouterPrices(ctx, baseURL, apiKey, headers, resolver) {
		if price.ModelName == "" || listed[price.ModelName] || !slices.Contains(price.SupportedParameters, "tools") {
			continue
		}
		listed[price.ModelName] = true
		var entry orcaRouterModel
		entry.ID = price.ModelName
		entry.Name = price.DisplayName
		entry.ContextLength = price.ContextLength
		entry.MaxCompletionTokens = price.MaxCompletionTokens
		entry.Architecture.InputModalities = price.InputModalities
		entry.SupportedParameters = price.SupportedParameters
		model := orcaRouterCatwalkModel(entry)
		if price.QuotaType == 0 {
			model.CostPer1MIn = price.ModelRatio * 2
			model.CostPer1MOut = model.CostPer1MIn * price.CompletionRatio
			model.CostPer1MInCached = model.CostPer1MIn * price.CacheRatio
		}
		models = append(models, model)
	}
	return models, nil
}

// orcaRouterPrices reads OrcaRouter's public price list, which sits beside
// the /v1 API. It only adds models, so any failure gives an empty list.
func orcaRouterPrices(ctx context.Context, baseURL, apiKey string, headers map[string]string, resolver Resolver) []orcaRouterPrice {
	resolvedBase, _ := resolver.ResolveValue(baseURL)
	resp, err := doRequest(ctx, http.MethodGet, strings.TrimSuffix(strings.TrimRight(resolvedBase, "/"), "/v1"), "/api/pricing", apiKey, headers, resolver, nil)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var list struct {
		Data []orcaRouterPrice `json:"data"`
	}
	if json.NewDecoder(resp.Body).Decode(&list) != nil {
		return nil
	}
	return list.Data
}

// orcaRouterCatwalkModel turns one OrcaRouter entry into a config entry:
// OpenRouter's conversion, OrcaRouter's own reply cap when top_provider
// has none, 200K when no size is listed, and Low, Medium, High for every
// model that thinks.
func orcaRouterCatwalkModel(entry orcaRouterModel) catwalk.Model {
	if entry.TopProvider.MaxCompletionTokens <= 0 {
		entry.TopProvider.MaxCompletionTokens = entry.MaxCompletionTokens
	}
	entry.Reasoning = nil
	model := openRouterCatwalkModel(entry.openRouterModel)
	model.ContextWindow = cmp.Or(model.ContextWindow, int64(200_000))
	if slices.Contains(entry.SupportedParameters, "reasoning") || slices.Contains(entry.SupportedParameters, "reasoning_effort") {
		model.CanReason = true
		model.ReasoningLevels = slices.Clone(orcaRouterEfforts)
		model.DefaultReasoningEffort = "medium"
	}
	return model
}

package discover

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"

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

// OrcaRouterCatalog lists OrcaRouter's models from its /models list, which
// is shaped like OpenRouter's. Every chat model is kept, OrcaRouter's own
// auto-pick routers too (owner, 2026-10-28); picture, video, voice and
// search-index models are left out. A model with no size listed gets
// 200K. The models come back as config entries, ready to be saved under
// the provider.
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
	for _, entry := range list.Data {
		if entry.ID == "" || !orcaRouterChat(entry) {
			continue
		}
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
		models = append(models, model)
	}
	return models, nil
}

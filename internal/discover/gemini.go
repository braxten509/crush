package discover

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"charm.land/catwalk/pkg/catwalk"
)

// geminiModelsResponse is one page of the Gemini API's models.list.
type geminiModelsResponse struct {
	Models        []geminiModel `json:"models"`
	NextPageToken string        `json:"nextPageToken"`
}

type geminiModel struct {
	Name                       string   `json:"name"`
	DisplayName                string   `json:"displayName"`
	Description                string   `json:"description"`
	InputTokenLimit            int64    `json:"inputTokenLimit"`
	OutputTokenLimit           int64    `json:"outputTokenLimit"`
	Thinking                   bool     `json:"thinking"`
	SupportedGenerationMethods []string `json:"supportedGenerationMethods"`
}

// geminiThinkingLevels are the Gemini API's thinking_level values. Gemini 2
// models take a thinking budget instead, which the provider options set.
var geminiThinkingLevels = []string{"low", "medium", "high"}

// discoverGeminiModels lists the models of a Gemini API key from Google's
// own models.list, which speaks neither the OpenAI path nor its auth.
// Models the catalog knows keep its prices and reasoning controls; the
// rest are described from the listing alone.
func discoverGeminiModels(ctx context.Context, cfg Config, resolver Resolver) ([]catwalk.Model, error) {
	baseURL, _ := resolver.ResolveValue(cfg.BaseURL)
	baseURL = strings.TrimSuffix(strings.TrimRight(cmp.Or(baseURL, "https://generativelanguage.googleapis.com"), "/"), "/v1beta")
	headers := maps.Clone(cfg.ExtraHeaders)
	if headers == nil {
		headers = map[string]string{}
	}
	headers["x-goog-api-key"] = cfg.APIKey

	known := map[string]catwalk.Model{}
	for _, provider := range cfg.KnownProviders {
		if provider.Type == catwalk.TypeGoogle && provider.ID == catwalk.InferenceProviderGemini {
			for _, model := range provider.Models {
				known[model.ID] = model
			}
		}
	}
	existing := map[string]bool{}
	for _, model := range cfg.ExistingModels {
		existing[model.ID] = true
	}
	result := slices.Clone(cfg.ExistingModels)

	pageToken := ""
	for {
		path := "/v1beta/models?pageSize=1000"
		if pageToken != "" {
			path += "&pageToken=" + url.QueryEscape(pageToken)
		}
		page, err := geminiModelsPage(ctx, cfg.ID, baseURL, path, headers, resolver)
		if err != nil {
			return nil, err
		}
		for _, entry := range page.Models {
			id := strings.TrimPrefix(entry.Name, "models/")
			if existing[id] || !geminiChatModel(entry) {
				continue
			}
			existing[id] = true
			result = append(result, geminiCatwalkModel(id, entry, known))
		}
		if page.NextPageToken == "" || page.NextPageToken == pageToken {
			return result, nil
		}
		pageToken = page.NextPageToken
	}
}

func geminiModelsPage(ctx context.Context, providerID, baseURL, path string, headers map[string]string, resolver Resolver) (geminiModelsResponse, error) {
	var page geminiModelsResponse
	resp, err := doRequest(ctx, http.MethodGet, baseURL, path, "", headers, resolver, nil)
	if err != nil {
		return page, fmt.Errorf("discover models for provider %s: %w", providerID, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return page, fmt.Errorf("discover models for provider %s: %s", providerID, resp.Status)
	}
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		return page, fmt.Errorf("discover models for provider %s: %w", providerID, err)
	}
	return page, nil
}

// geminiChatModel reports whether a listed model can run a Crush chat:
// it answers generateContent and thinks (speech, music, transcription
// and embedding models report no thinking), and doesn't make images.
func geminiChatModel(entry geminiModel) bool {
	if !entry.Thinking || !slices.Contains(entry.SupportedGenerationMethods, "generateContent") {
		return false
	}
	text := strings.ToLower(entry.Name + " " + entry.DisplayName + " " + entry.Description)
	return !strings.Contains(text, "image") && !strings.Contains(text, "nano banana") && !strings.Contains(text, "nano-banana")
}

func geminiCatwalkModel(id string, entry geminiModel, known map[string]catwalk.Model) catwalk.Model {
	if model, ok := known[id]; ok {
		model.Name = cmp.Or(model.Name, entry.DisplayName, id)
		model.ContextWindow = cmp.Or(model.ContextWindow, entry.InputTokenLimit)
		model.DefaultMaxTokens = cmp.Or(model.DefaultMaxTokens, entry.OutputTokenLimit)
		return model
	}
	model := catwalk.Model{
		ID:               id,
		Name:             cmp.Or(entry.DisplayName, id),
		ContextWindow:    cmp.Or(entry.InputTokenLimit, int64(200_000)),
		DefaultMaxTokens: min(cmp.Or(entry.OutputTokenLimit, int64(32_000)), 128_000),
		CanReason:        true,
		SupportsImages:   true,
	}
	if !strings.HasPrefix(id, "gemini-2") {
		model.ReasoningLevels = slices.Clone(geminiThinkingLevels)
		model.DefaultReasoningEffort = "medium"
	}
	return model
}

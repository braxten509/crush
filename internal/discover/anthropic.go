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

	"charm.land/catwalk/v3/pkg/catwalk"
)

const (
	anthropicDefaultBaseURL       = "https://api.anthropic.com"
	anthropicDefaultContextWindow = int64(200_000)
	anthropicDefaultMaxTokens     = int64(64_000)
	anthropicAPIVersion           = "2023-06-01"
)

// anthropicEffortLevels are the reasoning effort tiers Anthropic's /v1/models
// capability block may report, in order.
var anthropicEffortLevels = []string{"low", "medium", "high", "xhigh", "max"}

type anthropicModelsResponse struct {
	Data    []anthropicModel `json:"data"`
	HasMore bool             `json:"has_more"`
	LastID  string           `json:"last_id"`
}

type anthropicCapabilitySupport struct {
	Supported bool `json:"supported"`
}

type anthropicModel struct {
	ID             string `json:"id"`
	DisplayName    string `json:"display_name"`
	MaxInputTokens int64  `json:"max_input_tokens"`
	MaxTokens      int64  `json:"max_tokens"`
	Capabilities   *struct {
		ImageInput *anthropicCapabilitySupport `json:"image_input"`
		Thinking   *struct {
			Supported bool `json:"supported"`
			Types     *struct {
				Adaptive *anthropicCapabilitySupport `json:"adaptive"`
				Enabled  *anthropicCapabilitySupport `json:"enabled"`
				Disabled *anthropicCapabilitySupport `json:"disabled"`
			} `json:"types"`
		} `json:"thinking"`
		Effort *struct {
			Supported bool                        `json:"supported"`
			Low       *anthropicCapabilitySupport `json:"low"`
			Medium    *anthropicCapabilitySupport `json:"medium"`
			High      *anthropicCapabilitySupport `json:"high"`
			XHigh     *anthropicCapabilitySupport `json:"xhigh"`
			Max       *anthropicCapabilitySupport `json:"max"`
		} `json:"effort"`
	} `json:"capabilities"`
}

// isAnthropicProvider reports whether a provider is Anthropic itself, so its
// models come from Anthropic's paginated /v1/models API rather than a
// generic /models endpoint.
func isAnthropicProvider(cfg Config) bool {
	return catwalk.InferenceProvider(cfg.ID) == catwalk.InferenceProviderAnthropic
}

// discoverAnthropicModels reads all pages of GET {base}/v1/models using the
// provider's key and Anthropic version header. Models already known in the
// catalog or user config keep their pricing and attachment limits, while the
// API's list, order, names, token limits, and capabilities win.
func discoverAnthropicModels(ctx context.Context, cfg Config, resolver Resolver) ([]catwalk.Model, error) {
	resolvedBase, err := resolver.ResolveValue(cfg.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("discover models for provider %s: %w", cfg.ID, err)
	}
	baseURL := stripV1Suffix(cmp.Or(strings.TrimSpace(resolvedBase), anthropicDefaultBaseURL))
	if baseURL == "" {
		baseURL = anthropicDefaultBaseURL
	}

	apiKey, err := resolver.ResolveValue(cfg.APIKey)
	if err != nil {
		return nil, fmt.Errorf("discover models for provider %s: %w", cfg.ID, err)
	}

	headers := maps.Clone(cfg.ExtraHeaders)
	if headers == nil {
		headers = map[string]string{}
	}
	if apiKey != "" && !hasHeader(headers, "x-api-key") {
		headers["x-api-key"] = apiKey
	}
	if !hasHeader(headers, "anthropic-version") {
		headers["anthropic-version"] = anthropicAPIVersion
	}
	if !hasHeader(headers, "Accept") {
		headers["Accept"] = "application/json"
	}

	known := anthropicKnownModels(cfg)
	models := make([]catwalk.Model, 0)
	seen := make(map[string]bool)

	afterID := ""
	for {
		path := "/v1/models?limit=1000"
		if afterID != "" {
			path += "&after_id=" + url.QueryEscape(afterID)
		}
		page, err := anthropicModelsPage(ctx, cfg.ID, baseURL, path, headers, resolver)
		if err != nil {
			return nil, err
		}
		for _, entry := range page.Data {
			id := strings.TrimSpace(entry.ID)
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			models = append(models, anthropicCatwalkModel(id, entry, known))
		}
		if !page.HasMore {
			break
		}
		if page.LastID == "" || page.LastID == afterID {
			return nil, fmt.Errorf("discover models for provider %s: incomplete paginated response", cfg.ID)
		}
		afterID = page.LastID
	}
	return models, nil
}

func hasHeader(headers map[string]string, name string) bool {
	for k := range headers {
		if strings.EqualFold(k, name) {
			return true
		}
	}
	return false
}

func anthropicModelsPage(ctx context.Context, providerID, baseURL, path string, headers map[string]string, resolver Resolver) (anthropicModelsResponse, error) {
	var page anthropicModelsResponse
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

func anthropicKnownModels(cfg Config) map[string]catwalk.Model {
	known := make(map[string]catwalk.Model)
	for _, provider := range cfg.KnownProviders {
		if provider.ID == catwalk.InferenceProviderAnthropic {
			for _, model := range provider.Models {
				known[model.ID] = model
			}
		}
	}
	for _, model := range cfg.ExistingModels {
		known[model.ID] = model
	}
	return known
}

func anthropicCatwalkModel(id string, entry anthropicModel, known map[string]catwalk.Model) catwalk.Model {
	existing, knownModel := known[id]
	contextWindow := cmp.Or(entry.MaxInputTokens, existing.ContextWindow, anthropicDefaultContextWindow)
	maxTokens := cmp.Or(entry.MaxTokens, existing.DefaultMaxTokens, anthropicDefaultMaxTokens)
	capabilities := existing.Capabilities
	if entry.Capabilities != nil && entry.Capabilities.ImageInput != nil {
		capabilities.Vision = entry.Capabilities.ImageInput.Supported
	} else if !knownModel {
		capabilities.Vision = true
	}

	reasoning := existing.Reasoning
	if entry.Capabilities != nil && entry.Capabilities.Thinking != nil {
		t := entry.Capabilities.Thinking
		thinkingSupported := t.Supported || (t.Types != nil && ((t.Types.Adaptive != nil && t.Types.Adaptive.Supported) || (t.Types.Enabled != nil && t.Types.Enabled.Supported)))
		switch {
		case !thinkingSupported:
			reasoning.Thinking = catwalk.ThinkingNever
		case t.Types != nil && t.Types.Disabled != nil && !t.Types.Disabled.Supported:
			reasoning.Thinking = catwalk.ThinkingAlways
		default:
			reasoning.Thinking = catwalk.ThinkingToggleable
		}
	} else if !knownModel {
		reasoning.Thinking = catwalk.ThinkingToggleable
	}

	if entry.Capabilities != nil && entry.Capabilities.Effort != nil {
		e := entry.Capabilities.Effort
		if e.Supported {
			levels := supportedAnthropicEfforts(e.Low, e.Medium, e.High, e.XHigh, e.Max)
			if len(levels) > 0 {
				reasoning.EffortLevels = catwalk.NewEffortLevels(levels...)
				if reasoning.Thinking == "" || reasoning.Thinking == catwalk.ThinkingNever {
					reasoning.Thinking = catwalk.ThinkingToggleable
				}
				if !slices.Contains(levels, reasoning.DefaultEffortLevel) {
					switch {
					case slices.Contains(levels, "high"):
						reasoning.DefaultEffortLevel = "high"
					case slices.Contains(levels, "medium"):
						reasoning.DefaultEffortLevel = "medium"
					default:
						reasoning.DefaultEffortLevel = levels[0]
					}
				}
			}
		} else {
			reasoning.EffortLevels = nil
			reasoning.DefaultEffortLevel = ""
		}
	}

	return catwalk.Model{
		ID:               id,
		Type:             existing.Type,
		Name:             cmp.Or(entry.DisplayName, existing.Name, id),
		Pricing:          existing.Pricing,
		ContextWindow:    contextWindow,
		DefaultMaxTokens: maxTokens,
		Reasoning:        reasoning,
		Capabilities:     capabilities,
		MaxAttachments:   existing.MaxAttachments,
		Options:          existing.Options,
	}
}

func supportedAnthropicEfforts(low, medium, high, xhigh, max *anthropicCapabilitySupport) []string {
	slots := []*anthropicCapabilitySupport{low, medium, high, xhigh, max}
	var levels []string
	for i, name := range anthropicEffortLevels {
		if slots[i] != nil && slots[i].Supported {
			levels = append(levels, name)
		}
	}
	return levels
}

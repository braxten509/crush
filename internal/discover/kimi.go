package discover

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"charm.land/catwalk/v3/pkg/catwalk"
)

// Kimi's model list changes with the plan, so it comes from the provider
// itself rather than from a hand-kept list (owner, 2026-10-24). The
// endpoint is Anthropic-shaped: x-api-key and an anthropic-version. It
// also carries what each model takes: its context window and the effort
// levels it accepts.
const (
	kimiDefaultBaseURL = "https://api.kimi.com/coding"
	kimiContextWindow  = int64(262144)
	kimiMaxTokens      = int64(32768)
)

// discoverKimiModels reads GET {base}/v1/models with the provider's key.
// The API's list and order are the result: models the bundled catalog
// already knows keep their other details, and the API's own context and
// effort levels win where it sends them.
func discoverKimiModels(ctx context.Context, cfg Config, resolver Resolver) ([]catwalk.Model, error) {
	base := strings.TrimRight(cfg.BaseURL, "/")
	if base == "" {
		base = kimiDefaultBaseURL
	}
	apiKey, err := resolver.ResolveValue(cfg.APIKey)
	if err != nil {
		return nil, fmt.Errorf("discover models for provider %s: %w", cfg.ID, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/v1/models", nil)
	if err != nil {
		return nil, fmt.Errorf("discover models for provider %s: %w", cfg.ID, err)
	}
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("Accept", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("discover models for provider %s: %w", cfg.ID, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("discover models for provider %s: %s", cfg.ID, resp.Status)
	}

	var payload struct {
		Data []struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
			Limit       struct {
				Context int64 `json:"context"`
			} `json:"limit"`
			ContextWindow int64 `json:"context_window"`
			Reasoning     struct {
				Effort struct {
					Valid   []string `json:"valid"`
					Default string   `json:"default"`
				} `json:"effort"`
			} `json:"reasoning"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("discover models for provider %s: %w", cfg.ID, err)
	}

	known := make(map[string]catwalk.Model, len(cfg.ExistingModels))
	for _, m := range cfg.ExistingModels {
		known[m.ID] = m
	}

	models := make([]catwalk.Model, 0, len(payload.Data))
	seen := make(map[string]bool, len(payload.Data))
	for _, raw := range payload.Data {
		id := strings.TrimSpace(raw.ID)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		existing, knownModel := known[id]
		name := raw.DisplayName
		if name == "" {
			name = existing.Name
		}
		if name == "" {
			name = id
		}
		contextWindow := raw.Limit.Context
		if contextWindow <= 0 {
			contextWindow = raw.ContextWindow
		}
		if contextWindow <= 0 {
			contextWindow = existing.ContextWindow
		}
		if contextWindow <= 0 {
			contextWindow = kimiContextWindow
		}
		maxTokens := existing.DefaultMaxTokens
		if maxTokens <= 0 || maxTokens > contextWindow {
			maxTokens = min(kimiMaxTokens, contextWindow)
		}
		capabilities := existing.Capabilities
		capabilities.Vision = !knownModel || existing.Capabilities.Vision
		model := catwalk.Model{
			ID:               id,
			Type:             existing.Type,
			Name:             name,
			Pricing:          existing.Pricing,
			ContextWindow:    contextWindow,
			DefaultMaxTokens: maxTokens,
			Capabilities:     capabilities,
			MaxAttachments:   existing.MaxAttachments,
			Options:          existing.Options,
		}
		// The API's effort levels win; without them a known model keeps
		// the catalog's way of thinking.
		if knownModel {
			model.Reasoning.Thinking = existing.Reasoning.Thinking
		}
		if levels := raw.Reasoning.Effort.Valid; len(levels) > 0 {
			model.Reasoning.EffortLevels = catwalk.NewEffortLevels(levels...)
			if !model.CanReason() {
				model.Reasoning.Thinking = catwalk.ThinkingToggleable
			}
		}
		model.Reasoning.DefaultEffortLevel = raw.Reasoning.Effort.Default
		models = append(models, model)
	}
	return models, nil
}

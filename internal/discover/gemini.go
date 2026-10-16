package discover

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

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
	var listed []catwalk.Model

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
			listed = append(listed, geminiCatwalkModel(id, entry, known))
		}
		if page.NextPageToken == "" || page.NextPageToken == pageToken {
			break
		}
		pageToken = page.NextPageToken
	}
	// Google's list keeps models it no longer serves to this key (retired
	// for new users, or only reachable through its Interactions API or a
	// required tool), and marks them no differently. Ask each one.
	usable := checkGeminiModels(ctx, baseURL, headers, resolver, listed)
	for _, model := range listed {
		if usable[model.ID] {
			result = append(result, model)
		}
	}
	return result, nil
}

// geminiCheckTTL is how long a model's answer to the check is trusted.
const geminiCheckTTL = 7 * 24 * time.Hour

// geminiCheckCachePath is where check results are kept between starts.
// Empty keeps them in memory only, as tests do unless they set one.
var geminiCheckCachePath = func() string {
	if testing.Testing() {
		return ""
	}
	dir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "crush", "gemini-model-checks.json")
}

type geminiCheck struct {
	Usable  bool      `json:"usable"`
	Checked time.Time `json:"checked"`
}

var geminiCheckMu sync.Mutex

// checkGeminiModels reports which models answer a one-token request,
// asking only those without a recent answer. A refusal (400, 403, 404)
// hides a model; a rate limit, server error, timeout or network failure
// keeps it listed and unrecorded, so it is asked again next time.
// Results are kept per key, since Google's access differs between keys.
func checkGeminiModels(ctx context.Context, baseURL string, headers map[string]string, resolver Resolver, models []catwalk.Model) map[string]bool {
	geminiCheckMu.Lock()
	defer geminiCheckMu.Unlock()
	path := geminiCheckCachePath()
	cache := map[string]map[string]geminiCheck{}
	if path != "" {
		if data, err := os.ReadFile(path); err == nil {
			_ = json.Unmarshal(data, &cache)
		}
	}
	sum := sha256.Sum256([]byte(baseURL + "\x00" + headers["x-goog-api-key"]))
	scope := hex.EncodeToString(sum[:8])
	checks := cache[scope]
	if checks == nil {
		checks = map[string]geminiCheck{}
	}

	usable := make(map[string]bool, len(models))
	var stale []string
	for _, model := range models {
		if c, ok := checks[model.ID]; ok && time.Since(c.Checked) < geminiCheckTTL {
			usable[model.ID] = c.Usable
		} else {
			usable[model.ID] = true
			stale = append(stale, model.ID)
		}
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	changed := false
	for _, id := range stale {
		wg.Go(func() {
			refused, known := geminiRefuses(ctx, baseURL, id, headers, resolver)
			mu.Lock()
			defer mu.Unlock()
			usable[id] = !refused
			if known {
				checks[id] = geminiCheck{Usable: !refused, Checked: time.Now()}
				changed = true
			}
		})
	}
	wg.Wait()

	if changed && path != "" {
		cache[scope] = checks
		if data, err := json.Marshal(cache); err == nil && os.MkdirAll(filepath.Dir(path), 0o700) == nil {
			tmp := path + ".tmp"
			if os.WriteFile(tmp, data, 0o600) == nil {
				_ = os.Rename(tmp, path)
			}
		}
	}
	return usable
}

// geminiRefuses sends a model the smallest real request. known is false
// when the answer says nothing about the model itself.
func geminiRefuses(ctx context.Context, baseURL, id string, headers map[string]string, resolver Resolver) (refused, known bool) {
	body := map[string]any{
		"contents":         []any{map[string]any{"parts": []any{map[string]any{"text": "hi"}}}},
		"generationConfig": map[string]any{"maxOutputTokens": 1},
	}
	resp, err := doRequest(ctx, http.MethodPost, baseURL, "/v1beta/models/"+url.PathEscape(id)+":generateContent", "", headers, resolver, body)
	if err != nil {
		return false, false
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	switch {
	case resp.StatusCode == http.StatusOK:
		return false, true
	case resp.StatusCode == http.StatusBadRequest, resp.StatusCode == http.StatusForbidden, resp.StatusCode == http.StatusNotFound:
		return true, true
	default:
		return false, false
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

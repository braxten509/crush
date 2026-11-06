package providers

import (
	"cmp"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"charm.land/catwalk/v3/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/config/resolve"
)

// Endpoint is what TestConnection needs to know about a configured
// provider. APIKey and BaseURL may be unresolved templates.
type Endpoint struct {
	ID           string
	Type         catwalk.Type
	APIKey       string
	BaseURL      string
	ExtraHeaders map[string]string
}

// TestConnection checks that the endpoint accepts its API key, usually by
// listing its models. Providers without a suitable endpoint get a format
// check of the key instead, or no check at all. Values are expanded
// through resolver; the base URL only when a request is made.
func TestConnection(e Endpoint, resolver resolve.VariableResolver) error {
	apiKey, _ := resolver.ResolveValue(e.APIKey)
	testURL, headers, checked, err := connectionTestRequest(e, apiKey, resolver)
	if checked {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client := &http.Client{}
	req, err := http.NewRequestWithContext(ctx, "GET", testURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request for provider %s: %w", e.ID, err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	for k, v := range e.ExtraHeaders {
		req.Header.Set(k, v)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to create request for provider %s: %w", e.ID, err)
	}
	defer resp.Body.Close()

	switch catwalk.InferenceProvider(e.ID) {
	case catwalk.InferenceProviderZAI:
		if resp.StatusCode == http.StatusUnauthorized {
			return &StatusError{Provider: e.ID, StatusCode: resp.StatusCode, Status: resp.Status}
		}
	default:
		if resp.StatusCode != http.StatusOK {
			return &StatusError{Provider: e.ID, StatusCode: resp.StatusCode, Status: resp.Status}
		}
	}
	return nil
}

// StatusError is a connection test the provider refused. Status is the
// provider's own status line, so it is untrusted text.
type StatusError struct {
	Provider   string
	StatusCode int
	Status     string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("failed to connect to provider %s: %s", e.Provider, e.Status)
}

// KeyFormatError is a key judged by its format alone, without a request.
// Its text is Crush's own.
type KeyFormatError struct{ text string }

func (e *KeyFormatError) Error() string { return e.text }

// connectionTestRequest picks the URL and headers that test the
// endpoint's API key. When checked is true the key was judged without a
// request, and err is the verdict.
func connectionTestRequest(e Endpoint, apiKey string, resolver resolve.VariableResolver) (testURL string, headers map[string]string, checked bool, err error) {
	providerID := catwalk.InferenceProvider(e.ID)
	headers = make(map[string]string)

	// Providers with an SDK of their own are told by their catalog ID or,
	// for a custom entry configured before catwalk v3, by the v2 type that
	// named that SDK.
	is := func(t catwalk.Type, ids ...catwalk.InferenceProvider) bool {
		return (t != "" && e.Type == t) || slices.Contains(ids, providerID)
	}

	switch {
	case is("", catwalk.InferenceProviderMiniMax, catwalk.InferenceProviderMiniMaxChina):
		// NOTE: MiniMax has no good endpoint we can use to validate the API key.
		return "", nil, true, nil
	case is("", catwalk.InferenceProviderAlibabaSingapore):
		// NOTE: Alibaba has no good endpoint we can use to validate the API key.
		// Let's at least check the pattern.
		if !strings.HasPrefix(apiKey, "sk-") {
			return "", nil, true, &KeyFormatError{fmt.Sprintf("invalid API key format for provider %s", e.ID)}
		}
		return "", nil, true, nil
	case is("bedrock", catwalk.InferenceProviderBedrock, catwalk.InferenceProviderBedrockEurope):
		// NOTE: Bedrock has a `/foundation-models` endpoint that we could in
		// theory use, but apparently the authorization is region-specific,
		// so it's not so trivial.
		if strings.HasPrefix(apiKey, "ABSK") { // Bedrock API keys
			return "", nil, true, nil
		}
		return "", nil, true, &KeyFormatError{"not a valid bedrock api key"}
	case is("vercel", catwalk.InferenceProviderVercel):
		// NOTE: Vercel does not validate API keys on the `/models` endpoint.
		if strings.HasPrefix(apiKey, "vck_") { // Vercel API keys
			return "", nil, true, nil
		}
		return "", nil, true, &KeyFormatError{"not a valid vercel api key"}
	case is("google", catwalk.InferenceProviderGemini):
		baseURL, _ := resolver.ResolveValue(e.BaseURL)
		baseURL = cmp.Or(baseURL, "https://generativelanguage.googleapis.com")
		return baseURL + "/v1beta/models?key=" + url.QueryEscape(apiKey), headers, false, nil
	}

	// The models API endpoint is determined by the provider type, which
	// describes the API format the provider speaks.
	switch NormalizeType(e.Type) {
	case catwalk.TypeMessages:
		baseURL, _ := resolver.ResolveValue(e.BaseURL)
		baseURL = cmp.Or(baseURL, "https://api.anthropic.com")
		// The Messages API lists models at /v1/models. An endpoint given
		// with its /v1 (Anthropic's v2-era catalog entry) or without it
		// (Kimi Coding's https://api.kimi.com/coding) both land there.
		baseURL = strings.TrimSuffix(strings.TrimRight(baseURL, "/"), "/v1")
		testURL = baseURL + "/v1/models"

		headers["x-api-key"] = apiKey
		headers["anthropic-version"] = "2023-06-01"
	default:
		// Completions, responses, and custom provider types all expose
		// an OpenAI-style /models endpoint.
		baseURL, _ := resolver.ResolveValue(e.BaseURL)
		baseURL = cmp.Or(baseURL, "https://api.openai.com/v1")

		switch providerID {
		case catwalk.InferenceProviderOpenRouter:
			testURL = baseURL + "/credits"
		case catwalk.InferenceProviderOpenCodeGo:
			// OpenCode Go lists its models on the Zen endpoint.
			testURL = strings.Replace(baseURL, "/go", "", 1) + "/models"
		default:
			testURL = baseURL + "/models"
		}

		headers["Authorization"] = "Bearer " + apiKey
	}
	return testURL, headers, false, nil
}

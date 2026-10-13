package providers

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"charm.land/catwalk/pkg/catwalk"
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
			return fmt.Errorf("failed to connect to provider %s: %s", e.ID, resp.Status)
		}
	default:
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("failed to connect to provider %s: %s", e.ID, resp.Status)
		}
	}
	return nil
}

// connectionTestRequest picks the URL and headers that test the
// endpoint's API key. When checked is true the key was judged without a
// request, and err is the verdict.
func connectionTestRequest(e Endpoint, apiKey string, resolver resolve.VariableResolver) (testURL string, headers map[string]string, checked bool, err error) {
	providerID := catwalk.InferenceProvider(e.ID)
	headers = make(map[string]string)

	switch providerID {
	case catwalk.InferenceProviderMiniMax, catwalk.InferenceProviderMiniMaxChina:
		// NOTE: MiniMax has no good endpoint we can use to validate the API key.
		return "", nil, true, nil
	case catwalk.InferenceProviderAlibabaSingapore:
		// NOTE: Alibaba has no good endpoint we can use to validate the API key.
		// Let's at least check the pattern.
		if !strings.HasPrefix(apiKey, "sk-") {
			return "", nil, true, fmt.Errorf("invalid API key format for provider %s", e.ID)
		}
		return "", nil, true, nil
	}

	switch e.Type {
	case catwalk.TypeOpenAI, catwalk.TypeOpenAICompat, catwalk.TypeOpenRouter:
		baseURL, _ := resolver.ResolveValue(e.BaseURL)
		baseURL = cmp.Or(baseURL, "https://api.openai.com/v1")

		switch providerID {
		case catwalk.InferenceProviderOpenRouter:
			testURL = baseURL + "/credits"
		case catwalk.InferenceProviderOpenCodeGo:
			testURL = strings.Replace(baseURL, "/go", "", 1) + "/models"
		default:
			testURL = baseURL + "/models"
		}

		headers["Authorization"] = "Bearer " + apiKey
	case catwalk.TypeAnthropic:
		baseURL, _ := resolver.ResolveValue(e.BaseURL)
		baseURL = cmp.Or(baseURL, "https://api.anthropic.com/v1")

		switch providerID {
		case catwalk.InferenceKimiCoding:
			testURL = baseURL + "/v1/models"
		default:
			testURL = baseURL + "/models"
		}

		headers["x-api-key"] = apiKey
		headers["anthropic-version"] = "2023-06-01"
	case catwalk.TypeGoogle:
		baseURL, _ := resolver.ResolveValue(e.BaseURL)
		baseURL = cmp.Or(baseURL, "https://generativelanguage.googleapis.com")
		testURL = baseURL + "/v1beta/models?key=" + url.QueryEscape(apiKey)
	case catwalk.TypeBedrock:
		// NOTE: Bedrock has a `/foundation-models` endpoint that we could in
		// theory use, but apparently the authorization is region-specific,
		// so it's not so trivial.
		if strings.HasPrefix(apiKey, "ABSK") { // Bedrock API keys
			return "", nil, true, nil
		}
		return "", nil, true, errors.New("not a valid bedrock api key")
	case catwalk.TypeVercel:
		// NOTE: Vercel does not validate API keys on the `/models` endpoint.
		if strings.HasPrefix(apiKey, "vck_") { // Vercel API keys
			return "", nil, true, nil
		}
		return "", nil, true, errors.New("not a valid vercel api key")
	}
	return testURL, headers, false, nil
}

package providers

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"os"
	"slices"
	"strings"

	"charm.land/catwalk/pkg/catwalk"
	"charm.land/fantasy"
	"charm.land/fantasy/providers/anthropic"
	"charm.land/fantasy/providers/azure"
	"charm.land/fantasy/providers/bedrock"
	"charm.land/fantasy/providers/google"
	"charm.land/fantasy/providers/openai"
	"charm.land/fantasy/providers/openaicompat"
	"charm.land/fantasy/providers/openrouter"
	"charm.land/fantasy/providers/vercel"
	"github.com/charmbracelet/crush/internal/agent/cliagent"
	"github.com/charmbracelet/crush/internal/agent/hyper"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/discover"
	"github.com/charmbracelet/crush/internal/event"
	"github.com/charmbracelet/crush/internal/history"
	"github.com/charmbracelet/crush/internal/log"
	"github.com/charmbracelet/crush/internal/oauth"
	"github.com/charmbracelet/crush/internal/oauth/copilot"
	openaioauth "github.com/charmbracelet/crush/internal/oauth/openai"
	"github.com/charmbracelet/crush/internal/permission"
	openaisdk "github.com/charmbracelet/openai-go/option"
)

// Builder builds Fantasy providers and language models from Crush's
// provider configuration.
type Builder struct {
	Config *config.ConfigStore
	// Permissions and History are handed to agent CLI providers.
	Permissions permission.Service
	History     history.Service
}

// Copilot models that use the Responses API instead of Chat Completions.
var copilotResponsesModels = map[string]bool{
	"gpt-5.2":       true,
	"gpt-5.2-codex": true,
	"gpt-5.3-codex": true,
	"gpt-5.4":       true,
	"gpt-5.4-mini":  true,
	"gpt-5.5":       true,
	"gpt-5-mini":    true,
	"gpt-5.6-luna":  true,
	"gpt-5.6-terra": true,
	"gpt-5.6-sol":   true,
	"gpt-6-astra":   true,
	"gpt-6-luna":    true,
	"gpt-6-sol":     true,
	"gpt-6.1-sol":   true,
	"grok-4.5":      true,
	"grok-4.6":      true,
	"grok-4.7":      true,
}

// OpenCode models that use the Anthropic Messages API instead of Chat
// Completions. Which endpoint serves each model differs per provider, see
// https://opencode.ai/docs/zen and https://opencode.ai/docs/go.
func isOpenCodeMessagesModel(providerID, modelID string) bool {
	switch providerID {
	case string(catwalk.InferenceProviderOpenCodeGo):
		return strings.HasPrefix(modelID, "minimax-") ||
			strings.HasPrefix(modelID, "qwen3.6-") ||
			strings.HasPrefix(modelID, "qwen3.7-") ||
			strings.HasPrefix(modelID, "qwen3.8-")
	case string(catwalk.InferenceProviderOpenCodeZen):
		return strings.HasPrefix(modelID, "claude-") ||
			strings.HasPrefix(modelID, "qwen3.5-") ||
			strings.HasPrefix(modelID, "qwen3.6-") ||
			strings.HasPrefix(modelID, "qwen3.7-") ||
			strings.HasPrefix(modelID, "qwen3.8-")
	}
	return false
}

// OpenCode models that use the OpenAI Responses API instead of Chat
// Completions. See https://opencode.ai/docs/zen and https://opencode.ai/docs/go.
func isOpenCodeResponsesModel(modelID string) bool {
	return strings.HasPrefix(modelID, "gpt-") ||
		strings.HasPrefix(modelID, "grok-") ||
		strings.HasPrefix(modelID, "muse-spark-")
}

// LanguageModel returns the selected model of a built provider, wrapped
// the same way wherever Crush runs one (main agents, sub-agents and
// background tasks):
//   - OpenRouter models that support it run on their exacto variant;
//   - each request to an API provider is bounded by the configured request
//     timeout, so unreachable or hung providers fail instead of blocking a
//     session forever. The wrapper is applied per request, so retries get a
//     fresh budget each attempt. Agent CLIs manage their own timeouts, and
//     the session agent needs the unwrapped model to recognize them;
//   - Hyper completions no longer report the hypercredit balance, so Hyper
//     models fetch it from /v1/credits on every request.
func (b Builder) LanguageModel(ctx context.Context, provider fantasy.Provider, providerCfg config.ProviderConfig, selected config.SelectedModel) (fantasy.LanguageModel, error) {
	modelID := selected.Model
	if selected.Provider == openrouter.Name && isExactoSupported(modelID) {
		modelID += ":exacto"
	}
	model, err := provider.LanguageModel(ctx, modelID)
	if err != nil {
		return nil, err
	}
	if !config.IsCLIProviderType(providerCfg.Type) {
		model = newRequestTimeoutModel(model, b.Config.Config().Options.GetRequestTimeout())
		model = emptyStreamModel{LanguageModel: model}
	}
	if selected.Provider == hyper.Name {
		model = newHyperCreditsModel(model, b.hyperAPIKey)
	}
	return model, nil
}

// hyperAPIKey resolves the Hyper API key from the live config, so an
// OAuth token refreshed after the models were built is picked up by the
// next credits fetch.
func (b Builder) hyperAPIKey() string {
	return config.ResolveHyperAPIKey(b.Config.Config())
}

func (b Builder) buildAnthropicProvider(baseURL, apiKey string, headers map[string]string, providerID string, clients ...*http.Client) (fantasy.Provider, error) {
	var opts []anthropic.Option

	switch {
	case strings.HasPrefix(apiKey, "Bearer "):
		// NOTE: Prevent the SDK from picking up the API key from env.
		os.Setenv("ANTHROPIC_API_KEY", "")
		headers["Authorization"] = apiKey
	case providerID == string(catwalk.InferenceProviderMiniMax) || providerID == string(catwalk.InferenceProviderMiniMaxChina):
		// NOTE: Prevent the SDK from picking up the API key from env.
		os.Setenv("ANTHROPIC_API_KEY", "")
		headers["Authorization"] = "Bearer " + apiKey
	case apiKey != "":
		// X-Api-Key header
		opts = append(opts, anthropic.WithAPIKey(apiKey))
	}

	if len(headers) > 0 {
		opts = append(opts, anthropic.WithHeaders(headers))
	}

	if baseURL != "" {
		opts = append(opts, anthropic.WithBaseURL(baseURL))
	}

	if b.Config.Config().Options.Debug {
		httpClient := log.NewHTTPClient()
		opts = append(opts, anthropic.WithHTTPClient(httpClient))
	}
	if len(clients) > 0 {
		opts = append(opts, anthropic.WithHTTPClient(clients[0]))
	}
	return anthropic.New(opts...)
}

func (b Builder) buildOpenaiProvider(baseURL, apiKey string, headers map[string]string, token *oauth.Token) (fantasy.Provider, error) {
	opts := []openai.Option{
		openai.WithAPIKey(apiKey),
		openai.WithUseResponsesAPI(),
	}
	var httpClient *http.Client
	if b.Config.Config().Options.Debug {
		httpClient = log.NewHTTPClient()
	}
	if token != nil {
		// ChatGPT OAuth: requests go through the Codex backend, which
		// expects account headers and rejects some request fields, so
		// they pass through the Codex transport.
		if httpClient == nil {
			httpClient = &http.Client{}
		}
		httpClient.Transport = &openaioauth.Transport{
			Base:  httpClient.Transport,
			Token: token,
		}
	}
	if httpClient != nil {
		opts = append(opts, openai.WithHTTPClient(httpClient))
	}
	if len(headers) > 0 {
		opts = append(opts, openai.WithHeaders(headers))
	}
	if baseURL != "" {
		opts = append(opts, openai.WithBaseURL(baseURL))
	}
	return openai.New(opts...)
}

func (b Builder) buildOpenrouterProvider(_, apiKey string, headers map[string]string) (fantasy.Provider, error) {
	opts := []openrouter.Option{
		openrouter.WithAPIKey(apiKey),
	}
	if b.Config.Config().Options.Debug {
		httpClient := log.NewHTTPClient()
		opts = append(opts, openrouter.WithHTTPClient(httpClient))
	}
	if len(headers) > 0 {
		opts = append(opts, openrouter.WithHeaders(headers))
	}
	return openrouter.New(opts...)
}

func (b Builder) buildVercelProvider(_, apiKey string, headers map[string]string) (fantasy.Provider, error) {
	opts := []vercel.Option{
		vercel.WithAPIKey(apiKey),
	}
	if b.Config.Config().Options.Debug {
		httpClient := log.NewHTTPClient()
		opts = append(opts, vercel.WithHTTPClient(httpClient))
	}
	if len(headers) > 0 {
		opts = append(opts, vercel.WithHeaders(headers))
	}
	return vercel.New(opts...)
}

func (b Builder) buildOpenaiCompatProvider(baseURL, apiKey string, headers map[string]string, extraBody map[string]any, providerID string, isSubAgent bool, clients ...*http.Client) (fantasy.Provider, error) {
	opts := []openaicompat.Option{
		openaicompat.WithBaseURL(baseURL),
		openaicompat.WithAPIKey(apiKey),
	}

	// Set HTTP client based on provider and debug mode.
	var httpClient *http.Client
	switch providerID {
	case string(catwalk.InferenceProviderCopilot):
		opts = append(
			opts,
			openaicompat.WithUseResponsesAPI(),
			openaicompat.WithResponsesAPIFunc(func(modelID string) bool {
				return copilotResponsesModels[modelID]
			}),
		)
		httpClient = copilot.NewClient(isSubAgent, b.Config.Config().Options.Debug)

	case string(catwalk.InferenceProviderOpenCodeGo), string(catwalk.InferenceProviderOpenCodeZen):
		opts = append(
			opts,
			openaicompat.WithUseResponsesAPI(),
			openaicompat.WithResponsesAPIFunc(isOpenCodeResponsesModel),
		)

	case hyper.Name:
		// Hyper may route requests through a Prism model; capture the
		// router headers so the UI can show which model answered.
		opts = append(
			opts,
			openaicompat.WithLanguageModelOptions(
				openai.WithLanguageModelHeaderFunc(hyper.HeaderFunc),
			),
		)
	}
	if httpClient == nil && b.Config.Config().Options.Debug {
		httpClient = log.NewHTTPClient()
	}
	if len(clients) > 0 {
		httpClient = clients[0]
	}
	if httpClient != nil {
		opts = append(opts, openaicompat.WithHTTPClient(httpClient))
	}

	if len(headers) > 0 {
		opts = append(opts, openaicompat.WithHeaders(headers))
	}

	for extraKey, extraValue := range extraBody {
		opts = append(opts, openaicompat.WithSDKOptions(openaisdk.WithJSONSet(extraKey, extraValue)))
	}

	return openaicompat.New(opts...)
}

func (b Builder) buildAzureProvider(baseURL, apiKey string, headers map[string]string, options map[string]string) (fantasy.Provider, error) {
	opts := []azure.Option{
		azure.WithBaseURL(baseURL),
		azure.WithAPIKey(apiKey),
		azure.WithUseResponsesAPI(),
	}
	if b.Config.Config().Options.Debug {
		httpClient := log.NewHTTPClient()
		opts = append(opts, azure.WithHTTPClient(httpClient))
	}
	if options == nil {
		options = make(map[string]string)
	}
	if apiVersion, ok := options["apiVersion"]; ok {
		opts = append(opts, azure.WithAPIVersion(apiVersion))
	}
	if len(headers) > 0 {
		opts = append(opts, azure.WithHeaders(headers))
	}

	return azure.New(opts...)
}

func (b Builder) buildBedrockProvider(apiKey string, headers map[string]string, providerID string) (fantasy.Provider, error) {
	var opts []bedrock.Option
	if b.Config.Config().Options.Debug {
		httpClient := log.NewHTTPClient()
		opts = append(opts, bedrock.WithHTTPClient(httpClient))
	}
	if len(headers) > 0 {
		opts = append(opts, bedrock.WithHeaders(headers))
	}

	switch {
	case apiKey != "":
		opts = append(opts, bedrock.WithAPIKey(apiKey))
	case os.Getenv("AWS_BEARER_TOKEN_BEDROCK") != "":
		opts = append(opts, bedrock.WithAPIKey(os.Getenv("AWS_BEARER_TOKEN_BEDROCK")))
	default:
		// Skip, let the SDK do authentication.
	}

	switch providerID {
	case string(catwalk.InferenceProviderBedrockEurope):
		opts = append(opts, bedrock.WithRegion("eu-west-1"))
	default:
		opts = append(opts, bedrock.WithRegion("us-east-1"))
	}

	return bedrock.New(opts...)
}

func (b Builder) buildGoogleProvider(baseURL, apiKey string, headers map[string]string) (fantasy.Provider, error) {
	opts := []google.Option{
		google.WithBaseURL(baseURL),
		google.WithGeminiAPIKey(apiKey),
	}
	if b.Config.Config().Options.Debug {
		httpClient := log.NewHTTPClient()
		opts = append(opts, google.WithHTTPClient(httpClient))
	}
	if len(headers) > 0 {
		opts = append(opts, google.WithHeaders(headers))
	}
	return google.New(opts...)
}

func (b Builder) buildGoogleVertexProvider(headers map[string]string, options map[string]string) (fantasy.Provider, error) {
	opts := []google.Option{}
	if b.Config.Config().Options.Debug {
		httpClient := log.NewHTTPClient()
		opts = append(opts, google.WithHTTPClient(httpClient))
	}
	if len(headers) > 0 {
		opts = append(opts, google.WithHeaders(headers))
	}

	project := options["project"]
	location := options["location"]

	opts = append(opts, google.WithVertex(project, location))

	return google.New(opts...)
}

func (b Builder) isAnthropicThinking(model config.SelectedModel) bool {
	if model.Think {
		return true
	}
	opts, err := anthropic.ParseOptions(model.ProviderOptions)
	return err == nil && opts.Thinking != nil
}

// Provider builds the Fantasy provider that serves model from providerCfg.
// isSubAgent marks requests a sub-agent makes, which Copilot is told were
// started by an agent rather than the user.
func (b Builder) Provider(providerCfg config.ProviderConfig, model config.SelectedModel, isSubAgent bool) (fantasy.Provider, error) {
	if providerCfg.ID == config.AbacusProviderID {
		return b.buildAbacusProvider(providerCfg, model)
	}
	headers := maps.Clone(providerCfg.ExtraHeaders)
	if headers == nil {
		headers = make(map[string]string)
	}

	// handle special headers for anthropic
	if providerCfg.Type == anthropic.Name && b.isAnthropicThinking(model) {
		if v, ok := headers["anthropic-beta"]; ok {
			headers["anthropic-beta"] = v + ",interleaved-thinking-2025-05-14"
		} else {
			headers["anthropic-beta"] = "interleaved-thinking-2025-05-14"
		}
	}

	apiKey, _ := b.Config.Resolve(providerCfg.APIKey)
	baseURL, _ := b.Config.Resolve(providerCfg.BaseURL)

	switch providerCfg.ID {
	case string(catwalk.InferenceProviderOpenCodeGo), string(catwalk.InferenceProviderOpenCodeZen):
		if isOpenCodeMessagesModel(providerCfg.ID, model.Model) {
			baseURL = strings.TrimSuffix(baseURL, "/v1")
			return b.buildAnthropicProvider(baseURL, apiKey, headers, providerCfg.ID)
		}
	}

	if config.IsCLIProviderType(providerCfg.Type) {
		return cliagent.NewProvider(providerCfg.Type, b.Config.WorkingDir(), b.Config.Config().Options.DataDirectory, b.Permissions, b.History, model.ServiceTier, model.Ultracode), nil
	}

	switch providerCfg.Type {
	case openai.Name:
		// A ChatGPT login is the provider's single credential: every
		// request goes through the Codex backend with the OAuth token.
		token := providerCfg.OAuthToken
		if token != nil {
			baseURL = openaioauth.CodexBaseURL
			apiKey = token.AccessToken
			headers["originator"] = "crush"
			if token.AccountID != "" {
				headers["chatgpt-account-id"] = token.AccountID
			}
		}
		return b.buildOpenaiProvider(baseURL, apiKey, headers, token)
	case anthropic.Name:
		return b.buildAnthropicProvider(baseURL, apiKey, headers, providerCfg.ID)
	case openrouter.Name:
		return b.buildOpenrouterProvider(baseURL, apiKey, headers)
	case vercel.Name:
		return b.buildVercelProvider(baseURL, apiKey, headers)
	case azure.Name:
		return b.buildAzureProvider(baseURL, apiKey, headers, providerCfg.ExtraParams)
	case bedrock.Name:
		return b.buildBedrockProvider(apiKey, headers, providerCfg.ID)
	case google.Name:
		return b.buildGoogleProvider(baseURL, apiKey, headers)
	case "google-vertex":
		return b.buildGoogleVertexProvider(headers, providerCfg.ExtraParams)
	case openaicompat.Name, hyper.Name:
		switch providerCfg.ID {
		case hyper.Name:
			baseURL = hyper.BaseURL() + "/v1"
			headers["x-crush-id"] = event.GetID()
		case string(catwalk.InferenceProviderZAI):
			if providerCfg.ExtraBody == nil {
				providerCfg.ExtraBody = map[string]any{}
			}
			providerCfg.ExtraBody["tool_stream"] = true
		}
		return b.buildOpenaiCompatProvider(baseURL, apiKey, headers, providerCfg.ExtraBody, providerCfg.ID, isSubAgent)
	default:
		// Known custom providers (litellm, llamacpp, lmstudio, ollama,
		// omlx) are openai-compat under the hood.
		if discover.IsKnownCustomProvider(string(providerCfg.Type)) {
			return b.buildOpenaiCompatProvider(baseURL, apiKey, headers, providerCfg.ExtraBody, providerCfg.ID, isSubAgent)
		}
		return nil, fmt.Errorf("provider type not supported: %q", providerCfg.Type)
	}
}

func isExactoSupported(modelID string) bool {
	supportedModels := []string{
		"moonshotai/kimi-k2-0905",
		"deepseek/deepseek-v3.1-terminus",
		"z-ai/glm-4.6",
		"openai/gpt-oss-120b",
		"qwen/qwen3-coder",
	}
	return slices.Contains(supportedModels, modelID)
}

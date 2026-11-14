package providers

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"os"
	"slices"
	"strings"

	"charm.land/catwalk/v3/pkg/catwalk"
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

// legacyGoogleVertexType is the catwalk v2 provider type of Google Vertex
// AI. v2's other SDK types are each named after their Fantasy provider.
const legacyGoogleVertexType catwalk.Type = "google-vertex"

// legacySDKTypes are the catwalk v2 provider types that named an SDK of
// their own. catwalk v3 folds them into the endpoint types, so a custom
// provider still typed with one of them keeps the SDK it named, whatever
// its ID.
var legacySDKTypes = map[catwalk.Type]string{
	catwalk.Type(azure.Name):      azure.Name,
	catwalk.Type(bedrock.Name):    bedrock.Name,
	catwalk.Type(google.Name):     google.Name,
	legacyGoogleVertexType:        google.Name,
	catwalk.Type(openrouter.Name): openrouter.Name,
	catwalk.Type(vercel.Name):     vercel.Name,
}

// servingProvider returns the fantasy provider package that serves the
// given provider and model. Tinfoil always speaks chat completions through
// its verified client. Providers with bespoke SDKs (Bedrock, Azure, Google,
// Vertex AI, OpenRouter, Vercel, Hyper) are selected by ID, and a custom
// provider still typed with a catwalk v2 SDK type keeps that SDK.
// Everything else, gateways like Copilot and OpenCode included, is
// determined by the catwalk type, which describes the API endpoint format
// the model speaks and so the key its provider options are read from.
func servingProvider(providerCfg config.ProviderConfig, model catwalk.Model) string {
	if config.IsTinfoilProvider(providerCfg) {
		return openaicompat.Name
	}
	switch providerCfg.ID {
	case string(catwalk.InferenceProviderOpenAI):
		return openai.Name
	case string(catwalk.InferenceProviderBedrock), string(catwalk.InferenceProviderBedrockEurope):
		return bedrock.Name
	case string(catwalk.InferenceProviderAzure):
		return azure.Name
	case string(catwalk.InferenceProviderGemini), string(catwalk.InferenceProviderVertexAI):
		return google.Name
	case string(catwalk.InferenceProviderOpenRouter):
		return openrouter.Name
	case string(catwalk.InferenceProviderVercel):
		return vercel.Name
	case hyper.Name:
		return hyper.Name
	}
	if name, ok := legacySDKTypes[providerCfg.Type]; ok {
		return name
	}

	switch config.NormalizeProviderType(model.EffectiveType(providerCfg.Type)) {
	case catwalk.TypeResponses:
		return openai.Name
	case catwalk.TypeMessages:
		return anthropic.Name
	case catwalk.TypeCompletions:
		return openaicompat.Name
	default:
		// Custom provider types (litellm, ollama, lmstudio, ...) are
		// handled by the caller through IsKnownCustomProvider.
		return string(providerCfg.Type)
	}
}

// modelWireType is the endpoint type the selected model speaks: its
// catalog entry's type, or the provider's when the entry sets none or the
// provider does not list the model.
func modelWireType(providerCfg config.ProviderConfig, modelID string) catwalk.Type {
	for _, models := range [][]catwalk.Model{providerCfg.Models, providerCfg.ChatGPTModels, providerCfg.GrokModels} {
		for _, m := range models {
			if m.ID == modelID {
				return m.EffectiveType(providerCfg.Type)
			}
		}
	}
	return providerCfg.Type
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

func (b Builder) buildOpenaiProvider(baseURL, apiKey string, headers map[string]string, token *oauth.Token, wireType catwalk.Type) (fantasy.Provider, error) {
	opts := []openai.Option{
		openai.WithAPIKey(apiKey),
		openai.WithUseResponsesAPI(),
		// Whether a model speaks the Responses API comes from catwalk's
		// model type, not from a model ID heuristic.
		openai.WithResponsesAPIFunc(func(string) bool {
			return wireType == catwalk.TypeResponses
		}),
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
	if headers == nil {
		headers = make(map[string]string)
	}
	if _, ok := headers["HTTP-Referer"]; !ok {
		headers["HTTP-Referer"] = "https://charm.land/crush"
	}
	if _, ok := headers["X-Title"]; !ok {
		headers["X-Title"] = "Crush"
	}
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

func (b Builder) buildOpenaiCompatProvider(baseURL, apiKey string, headers map[string]string, extraBody map[string]any, providerID string, isSubAgent bool, wireType catwalk.Type, clients ...*http.Client) (fantasy.Provider, error) {
	opts := []openaicompat.Option{
		openaicompat.WithBaseURL(baseURL),
		openaicompat.WithAPIKey(apiKey),
	}

	// Set HTTP client based on provider and debug mode.
	var httpClient *http.Client
	// Gateway providers route per model between chat completions and the
	// responses API based on the model's catwalk type.
	responsesFunc := func(string) bool { return wireType == catwalk.TypeResponses }
	switch providerID {
	case string(catwalk.InferenceProviderCopilot):
		opts = append(
			opts,
			openaicompat.WithUseResponsesAPI(),
			openaicompat.WithResponsesAPIFunc(responsesFunc),
		)
		httpClient = copilot.NewClient(isSubAgent, b.Config.Config().Options.Debug)

	case string(catwalk.InferenceProviderOpenCodeGo), string(catwalk.InferenceProviderOpenCodeZen):
		opts = append(
			opts,
			openaicompat.WithUseResponsesAPI(),
			openaicompat.WithResponsesAPIFunc(responsesFunc),
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

// Provider builds the Fantasy provider that serves model from providerCfg,
// on the endpoint type the model speaks: its catalog entry's type, or the
// provider's. isSubAgent marks requests a sub-agent makes, which Copilot is
// told were started by an agent rather than the user.
func (b Builder) Provider(providerCfg config.ProviderConfig, model config.SelectedModel, isSubAgent bool) (fantasy.Provider, error) {
	return b.buildProvider(providerCfg, model, isSubAgent, modelWireType(providerCfg, model.Model))
}

// buildProvider builds the provider for model speaking the wireType
// endpoint. It picks the SDK the same way servingProvider picks the key of
// the call's options, so the two always agree.
func (b Builder) buildProvider(providerCfg config.ProviderConfig, model config.SelectedModel, isSubAgent bool, wireType catwalk.Type) (fantasy.Provider, error) {
	if providerCfg.ID == config.AbacusProviderID {
		return b.buildAbacusProvider(providerCfg, model)
	}
	wireType = config.NormalizeProviderType(wireType)
	headers := maps.Clone(providerCfg.ExtraHeaders)
	if headers == nil {
		headers = make(map[string]string)
	}

	// handle special headers for anthropic
	if wireType == catwalk.TypeMessages && b.isAnthropicThinking(model) {
		if v, ok := headers["anthropic-beta"]; ok {
			headers["anthropic-beta"] = v + ",interleaved-thinking-2025-05-14"
		} else {
			headers["anthropic-beta"] = "interleaved-thinking-2025-05-14"
		}
	}

	if wireType == catwalk.TypeMessages && config.AnthropicProgressUpdates(providerCfg.ID, model.Model) {
		if v := headers["anthropic-beta"]; v != "" {
			headers["anthropic-beta"] = v + "," + config.AnthropicProgressUpdatesBeta
		} else {
			headers["anthropic-beta"] = config.AnthropicProgressUpdatesBeta
		}
	}
	if wireType == catwalk.TypeMessages && config.AnthropicThinkingBinding(providerCfg.ID, model.Model) {
		if v := headers["anthropic-beta"]; v != "" {
			headers["anthropic-beta"] = v + "," + config.AnthropicThinkingBindingBeta
		} else {
			headers["anthropic-beta"] = config.AnthropicThinkingBindingBeta
		}
	}

	apiKey, _ := b.Config.Resolve(providerCfg.APIKey)
	baseURL, _ := b.Config.Resolve(providerCfg.BaseURL)

	if config.IsCLIProviderType(providerCfg.Type) {
		options := b.Config.Config().Options
		return cliagent.NewProvider(providerCfg.Type, b.Config.WorkingDir(), options.DataDirectory, b.Permissions, b.History, model.ServiceTier, model.Ultracode, model.Account, options.OpenCodeZenFreeDeepSeekFullContext), nil
	}

	// Tinfoil serves every model on chat completions, and only through the
	// client that checks its secure enclave, whatever a catalog entry says.
	if config.IsTinfoilProvider(providerCfg) {
		client, err := tinfoilHTTPClient(baseURL)
		if err != nil {
			return nil, err
		}
		return b.buildOpenaiCompatProvider(baseURL, apiKey, headers, providerCfg.ExtraBody, providerCfg.ID, isSubAgent, catwalk.TypeCompletions, client)
	}

	// Bespoke SDKs, picked by provider ID or by a catwalk v2 SDK type.
	switch servingProvider(providerCfg, catwalk.Model{Type: wireType}) {
	case bedrock.Name:
		return b.buildBedrockProvider(apiKey, headers, providerCfg.ID)
	case azure.Name:
		return b.buildAzureProvider(baseURL, apiKey, headers, providerCfg.ExtraParams)
	case google.Name:
		if providerCfg.ID == string(catwalk.InferenceProviderVertexAI) || providerCfg.Type == legacyGoogleVertexType {
			return b.buildGoogleVertexProvider(headers, providerCfg.ExtraParams)
		}
		return b.buildGoogleProvider(baseURL, apiKey, headers)
	case openrouter.Name:
		return b.buildOpenrouterProvider(baseURL, apiKey, headers)
	case vercel.Name:
		return b.buildVercelProvider(baseURL, apiKey, headers)
	}

	switch providerCfg.ID {
	case string(catwalk.InferenceProviderCopilot),
		string(catwalk.InferenceProviderOpenCodeGo), string(catwalk.InferenceProviderOpenCodeZen):
		// Messages models on the OpenCode gateways are served by the
		// Anthropic Messages API at the endpoint root, without the /v1
		// prefix the chat completions and responses endpoints use.
		if wireType == catwalk.TypeMessages {
			baseURL = strings.TrimSuffix(baseURL, "/v1")
			return b.buildAnthropicProvider(baseURL, apiKey, headers, providerCfg.ID)
		}
		return b.buildOpenaiCompatProvider(baseURL, apiKey, headers, providerCfg.ExtraBody, providerCfg.ID, isSubAgent, wireType)
	case string(catwalk.InferenceProviderOpenAI):
		return b.buildOpenaiAPIProvider(baseURL, apiKey, headers, providerCfg, model, wireType)
	}

	switch wireType {
	case catwalk.TypeMessages:
		return b.buildAnthropicProvider(baseURL, apiKey, headers, providerCfg.ID)
	case catwalk.TypeResponses:
		return b.buildOpenaiAPIProvider(baseURL, apiKey, headers, providerCfg, model, wireType)
	case catwalk.TypeCompletions, catwalk.Type(hyper.Name):
		// Hyper's provider advertises its own type rather than a wire
		// type; it speaks chat completions.
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
		return b.buildOpenaiCompatProvider(baseURL, apiKey, headers, providerCfg.ExtraBody, providerCfg.ID, isSubAgent, wireType)
	default:
		// Known custom providers (litellm, llamacpp, lmstudio, ollama,
		// omlx) are openai-compat under the hood.
		if discover.IsKnownCustomProvider(string(providerCfg.Type)) {
			return b.buildOpenaiCompatProvider(baseURL, apiKey, headers, providerCfg.ExtraBody, providerCfg.ID, isSubAgent, wireType)
		}
		return nil, fmt.Errorf("provider type not supported: %q", providerCfg.Type)
	}
}

// buildOpenaiAPIProvider serves OpenAI's API. A ChatGPT login is the
// provider's single credential: every request goes through the Codex
// backend with the OAuth token. Without one, the Codex API's models go out
// as their real model on their service tier.
func (b Builder) buildOpenaiAPIProvider(baseURL, apiKey string, headers map[string]string, providerCfg config.ProviderConfig, model config.SelectedModel, wireType catwalk.Type) (fantasy.Provider, error) {
	token := providerCfg.OAuthToken
	if token == nil && config.IsCodexAPIModel(model.Model) {
		return b.buildCodexAPIProvider(baseURL, apiKey, headers, providerCfg, model)
	}
	if token != nil {
		baseURL = openaioauth.CodexBaseURL
		apiKey = token.AccessToken
		headers["originator"] = "crush"
		if token.AccountID != "" {
			headers["chatgpt-account-id"] = token.AccountID
		}
	}
	return b.buildOpenaiProvider(baseURL, apiKey, headers, token, wireType)
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

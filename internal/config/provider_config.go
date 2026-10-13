package config

import (
	"maps"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/config/providers"
	"github.com/charmbracelet/crush/internal/oauth"
	"github.com/charmbracelet/crush/internal/oauth/copilot"
)

type ProviderConfig struct {
	// The provider's id.
	ID string `json:"id,omitempty" jsonschema:"description=Unique identifier for the provider,example=openai"`
	// The provider's name, used for display purposes.
	Name string `json:"name,omitempty" jsonschema:"description=Human-readable name for the provider,example=OpenAI"`
	// The provider's API endpoint.
	BaseURL string `json:"base_url,omitempty" jsonschema:"description=Base URL for the provider's API,format=uri,example=https://api.openai.com/v1"`
	// The provider type, e.g. "openai", "anthropic", etc. if empty it defaults to openai.
	Type catwalk.Type `json:"type,omitempty" jsonschema:"description=Provider type that determines the API format,default=openai"`
	// The provider's API key.
	APIKey string `json:"api_key,omitempty" jsonschema:"description=API key for authentication with the provider,example=$OPENAI_API_KEY"`
	// The original API key template before resolution (for re-resolution on auth errors).
	APIKeyTemplate string `json:"-"`
	// OAuthToken for providers that use OAuth2 authentication.
	OAuthToken *oauth.Token `json:"oauth,omitempty" jsonschema:"description=OAuth2 token for authentication with the provider"`
	// Marks the provider as disabled.
	Disable bool `json:"disable,omitempty" jsonschema:"description=Whether this provider is disabled,default=false"`

	// Custom system prompt prefix.
	SystemPromptPrefix string `json:"system_prompt_prefix,omitempty" jsonschema:"description=Custom prefix to add to system prompts for this provider"`

	// Extra headers to send with each request to the provider. Values
	// run through shell expansion at config-load time, so $VAR and
	// $(cmd) work the same way they do in MCP headers. A header whose
	// value resolves to the empty string (unset bare $VAR under
	// lenient nounset, $(echo), or literal "") is omitted from the
	// outgoing request rather than sent as "Header:".
	ExtraHeaders map[string]string `json:"extra_headers,omitempty" jsonschema:"description=Additional HTTP headers to send with requests"`
	// ExtraBody is merged verbatim into OpenAI-compatible request
	// bodies. String values are NOT shell-expanded: this is a plain
	// JSON passthrough so that arbitrary provider-extension fields
	// (numbers, nested objects, booleans) round-trip without a
	// recursive walker guessing at intent. If you need an env-var-
	// driven value at request time, put it in extra_headers, or in
	// the provider's top-level api_key / base_url, all of which do
	// expand.
	ExtraBody map[string]any `json:"extra_body,omitempty" jsonschema:"description=Additional fields to include in request bodies\\, only works with openai-compatible providers"`

	ProviderOptions map[string]any `json:"provider_options,omitempty" jsonschema:"description=Additional provider-specific options for this provider"`

	// Used to pass extra parameters to the provider.
	ExtraParams map[string]string `json:"-"`

	// AWSAuthRefresh is a shell command run when Bedrock returns a
	// credential error. Output is discarded to avoid corrupting the TUI.
	AWSAuthRefresh string `json:"aws_auth_refresh,omitempty" jsonschema:"description=Shell command to run when AWS credentials expire (Bedrock only)."`

	// Skip cost accumulation for this provider when using subscription or flat rate billing.
	FlatRate bool `json:"flat_rate,omitempty" jsonschema:"description=Flat-rate mode for this provider"`

	// AutoDiscoverModels controls model discovery via /v1/models endpoint.
	// When Models is empty and this is nil or true, Crush auto-discovers
	// models. When true and Models is non-empty, discovered models are
	// merged in (user-specified models take precedence). When false,
	// only explicitly listed models are used.
	AutoDiscoverModels *bool `json:"discover_models,omitempty" jsonschema:"description=Auto-discover models from /v1/models endpoint. When true with existing models they are merged (yours win),default=true"`

	// The provider models
	Models []catwalk.Model `json:"models,omitempty" jsonschema:"description=List of models available from this provider"`

	// ChatGPTModels lists the models the ChatGPT (Codex) backend grants
	// when the provider is authenticated with a ChatGPT account. It is
	// the provider's whole catalog in that case: the API-key models in
	// Models are not served by the subscription.
	ChatGPTModels []catwalk.Model `json:"chatgpt_models,omitempty" jsonschema:"-"`

	// GrokModels lists the models the Grok plan grants when the provider
	// is authenticated with a Grok account. Like ChatGPTModels, it is
	// the provider's whole catalog in that case.
	GrokModels []catwalk.Model `json:"grok_models,omitempty" jsonschema:"-"`
}

// AvailableModels returns the catalog served by the provider's active credentials.
func (p ProviderConfig) AvailableModels() []catwalk.Model {
	if p.OAuthToken != nil {
		switch catwalk.InferenceProvider(p.ID) {
		case catwalk.InferenceProviderOpenAI:
			return p.ChatGPTModels
		case catwalk.InferenceProviderXAI:
			return p.GrokModels
		}
	}
	return p.Models
}

// ToProvider converts the [ProviderConfig] to a [catwalk.Provider].
func (c *ProviderConfig) ToProvider() catwalk.Provider {
	// Convert config provider to provider.Provider format
	provider := catwalk.Provider{
		Name:   c.Name,
		ID:     catwalk.InferenceProvider(c.ID),
		Models: make([]catwalk.Model, len(c.Models)),
	}

	// Convert models
	for i, model := range c.Models {
		provider.Models[i] = catwalk.Model{
			ID:                     model.ID,
			Name:                   model.Name,
			CostPer1MIn:            model.CostPer1MIn,
			CostPer1MOut:           model.CostPer1MOut,
			CostPer1MInCached:      model.CostPer1MInCached,
			CostPer1MOutCached:     model.CostPer1MOutCached,
			ContextWindow:          model.ContextWindow,
			DefaultMaxTokens:       model.DefaultMaxTokens,
			CanReason:              model.CanReason,
			ReasoningLevels:        model.ReasoningLevels,
			DefaultReasoningEffort: model.DefaultReasoningEffort,
			SupportsImages:         model.SupportsImages,
		}
	}

	return provider
}

func (c *ProviderConfig) SetupGitHubCopilot() {
	maps.Copy(c.ExtraHeaders, copilot.Headers())
}

// HasAPIKey reports whether the provider's api_key resolves to a usable
// credential. The stored value is often an unresolved template like
// $OPENAI_API_KEY, which is not a credential until the variable exists.
func (c *ProviderConfig) HasAPIKey(resolver VariableResolver) bool {
	if c.APIKey == "" {
		return false
	}
	v, err := resolver.ResolveValue(c.APIKey)
	return err == nil && v != ""
}

// TestConnection checks that the provider accepts its API key. See
// providers.TestConnection.
func (c *ProviderConfig) TestConnection(resolver VariableResolver) error {
	return providers.TestConnection(providers.Endpoint{
		ID:           c.ID,
		Type:         c.Type,
		APIKey:       c.APIKey,
		BaseURL:      c.BaseURL,
		ExtraHeaders: c.ExtraHeaders,
	}, resolver)
}

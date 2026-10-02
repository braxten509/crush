package agent

import (
	"maps"

	"charm.land/catwalk/pkg/catwalk"
	"charm.land/fantasy"
	"charm.land/fantasy/providers/openai"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/openai-go/option"
)

func (c *coordinator) buildAbacusProvider(provider config.ProviderConfig, selected config.SelectedModel) (fantasy.Provider, error) {
	model := catwalk.Model{ID: selected.Model}
	for _, candidate := range provider.Models {
		if candidate.ID == selected.Model {
			model = candidate
			break
		}
	}
	effective := config.AbacusProviderForModel(provider, model)
	baseURL, err := c.cfg.Resolve(effective.BaseURL)
	if err != nil {
		return nil, err
	}
	apiKey, err := c.cfg.Resolve(provider.APIKey)
	if err != nil {
		return nil, err
	}
	headers := maps.Clone(provider.ExtraHeaders)
	if headers == nil {
		headers = make(map[string]string)
	}
	client := c.abacusHTTPClient(provider, model, selected)
	if effective.Type == catwalk.TypeAnthropic {
		return c.buildAnthropicProvider(baseURL, apiKey, headers, provider.ID, client)
	}
	if config.AbacusAPIFormat(model) != "responses" {
		return c.buildOpenaiCompatProvider(baseURL, apiKey, headers, provider.ExtraBody, provider.ID, false, client)
	}
	opts := []openai.Option{openai.WithAPIKey(apiKey), openai.WithBaseURL(baseURL), openai.WithUseResponsesAPI(), openai.WithResponsesAPIFunc(func(string) bool { return true }), openai.WithHTTPClient(client)}
	if len(headers) > 0 {
		opts = append(opts, openai.WithHeaders(headers))
	}
	var sdkOptions []option.RequestOption
	for key, value := range provider.ExtraBody {
		sdkOptions = append(sdkOptions, option.WithJSONSet(key, value))
	}
	// Fantasy's model table predates some of Abacus's Responses models. Keep
	// controls intact even when that table would omit reasoning or priority.
	options := getProviderOptions(Model{CatwalkCfg: model, ModelCfg: selected}, provider)
	if responses, ok := options[openai.Name].(*openai.ResponsesProviderOptions); ok {
		if responses.ReasoningEffort != nil {
			sdkOptions = append(sdkOptions, option.WithJSONSet("reasoning.effort", string(*responses.ReasoningEffort)))
		}
		if responses.ReasoningSummary != nil {
			sdkOptions = append(sdkOptions, option.WithJSONSet("reasoning.summary", *responses.ReasoningSummary))
		}
		if responses.ServiceTier != nil {
			sdkOptions = append(sdkOptions, option.WithJSONSet("service_tier", string(*responses.ServiceTier)))
		}
	}
	if len(sdkOptions) > 0 {
		opts = append(opts, openai.WithSDKOptions(sdkOptions...))
	}
	return openai.New(opts...)
}

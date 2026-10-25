package providers

import (
	"charm.land/catwalk/pkg/catwalk"
	"charm.land/fantasy"
	"charm.land/fantasy/providers/openai"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/log"
	"github.com/charmbracelet/openai-go/option"
)

// buildCodexAPIProvider serves the Codex API's models on the Responses API.
// An ultrafast entry goes out as its real model on the ultrafast tier, and,
// as for Abacus, the effort is set in the body because Fantasy's model table
// does not count GPT-6 as a reasoning model and would drop it.
func (b Builder) buildCodexAPIProvider(baseURL, apiKey string, headers map[string]string, provider config.ProviderConfig, selected config.SelectedModel) (fantasy.Provider, error) {
	model := catwalk.Model{ID: selected.Model}
	for _, candidate := range provider.Models {
		if candidate.ID == selected.Model {
			model = candidate
			break
		}
	}
	realModel, tier := config.CodexAPIRequestModel(selected.Model)
	sdkOptions := []option.RequestOption{option.WithJSONSet("model", realModel)}
	if tier != "" {
		sdkOptions = append(sdkOptions, option.WithJSONSet("service_tier", tier))
	}
	if responses, ok := Options(model, selected, provider)[openai.Name].(*openai.ResponsesProviderOptions); ok {
		if responses.ReasoningEffort != nil {
			sdkOptions = append(sdkOptions, option.WithJSONSet("reasoning.effort", string(*responses.ReasoningEffort)))
		}
		if responses.ReasoningSummary != nil {
			sdkOptions = append(sdkOptions, option.WithJSONSet("reasoning.summary", *responses.ReasoningSummary))
		}
	}
	opts := []openai.Option{openai.WithAPIKey(apiKey), openai.WithUseResponsesAPI(), openai.WithSDKOptions(sdkOptions...)}
	if baseURL != "" {
		opts = append(opts, openai.WithBaseURL(baseURL))
	}
	if b.Config.Config().Options.Debug {
		opts = append(opts, openai.WithHTTPClient(log.NewHTTPClient()))
	}
	if len(headers) > 0 {
		opts = append(opts, openai.WithHeaders(headers))
	}
	return openai.New(opts...)
}

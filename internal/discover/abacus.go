package discover

import (
	"cmp"
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"sync"

	"charm.land/catwalk/pkg/catwalk"
	"charm.land/catwalk/pkg/embedded"
)

func abacusModel(raw json.RawMessage) (catwalk.Model, bool) {
	var entry struct {
		ID                   string   `json:"id"`
		DisplayName          string   `json:"display_name"`
		ModelType            string   `json:"model_type"`
		APIFormats           []string `json:"api_formats"`
		InputModalities      []string `json:"input_modalities"`
		ContextLength        int64    `json:"context_length"`
		MaxCompletionTokens  int64    `json:"max_completion_tokens"`
		InputTokenRate       string   `json:"input_token_rate"`
		OutputTokenRate      string   `json:"output_token_rate"`
		CachedInputTokenRate string   `json:"cached_input_token_rate"`
		Thinking             bool     `json:"thinking"`
		Tools                bool     `json:"tools"`
	}
	if json.Unmarshal(raw, &entry) != nil || entry.ID == "" || entry.ModelType != "text_generation" || !entry.Tools || len(entry.APIFormats) == 0 {
		return catwalk.Model{}, false
	}
	format := "openai"
	if slices.Contains(entry.APIFormats, "anthropic") {
		format = "anthropic"
	} else if slices.Contains(entry.APIFormats, "responses") && !strings.HasPrefix(entry.ID, "route-llm") {
		format = "responses"
	} else if !slices.Contains(entry.APIFormats, "openai") {
		return catwalk.Model{}, false
	}
	rate := func(value string) float64 {
		parsed, _ := strconv.ParseFloat(value, 64)
		return parsed * 1_000_000
	}
	model := catwalk.Model{
		ID:                entry.ID,
		Name:              cmp.Or(entry.DisplayName, entry.ID),
		ContextWindow:     cmp.Or(entry.ContextLength, int64(200_000)),
		DefaultMaxTokens:  min(cmp.Or(entry.MaxCompletionTokens, int64(32_000)), 128_000),
		CostPer1MIn:       rate(entry.InputTokenRate),
		CostPer1MOut:      rate(entry.OutputTokenRate),
		CostPer1MInCached: rate(entry.CachedInputTokenRate),
		SupportsImages:    slices.Contains(entry.InputModalities, "image"),
		CanReason:         entry.Thinking || strings.HasSuffix(entry.ID, "-thinking"),
		Options:           catwalk.ModelOptions{ProviderOptions: map[string]any{"abacus_api_format": format}},
	}
	applyAbacusControls(&model)
	return model, true
}

var abacusKnownModels = sync.OnceValue(func() map[string]catwalk.Model {
	models := map[string]catwalk.Model{}
	for _, provider := range embedded.GetAll() {
		switch string(provider.ID) {
		case "anthropic", "openai", "gemini", "xai", "deepseek", "zai":
			for _, model := range provider.Models {
				models[strings.ToLower(model.ID)] = model
			}
		}
	}
	return models
})

func applyAbacusControls(model *catwalk.Model) {
	id := strings.ToLower(strings.TrimSuffix(model.ID, "-thinking"))
	knownID := id
	switch {
	case strings.HasPrefix(id, "claude-opus-5-5"):
		knownID = "claude-opus-5"
	case strings.HasPrefix(id, "claude-sonnet-5-5"):
		knownID = "claude-sonnet-5"
	case strings.HasPrefix(id, "gpt-6"):
		knownID = "gpt-6-astra"
	case strings.Contains(id, "deepseek-v4"):
		if strings.Contains(id, "pro") {
			knownID = "deepseek-v4-pro"
		} else {
			knownID = "deepseek-v4-flash"
		}
	case strings.HasPrefix(id, "zai-org/"):
		knownID = strings.TrimPrefix(id, "zai-org/")
	}
	if known, ok := abacusKnownModels()[knownID]; ok {
		model.CanReason = known.CanReason || model.CanReason
		model.ReasoningLevels = slices.Clone(known.ReasoningLevels)
		model.DefaultReasoningEffort = known.DefaultReasoningEffort
	}
	if strings.HasPrefix(id, "claude-opus-5-5") {
		model.DefaultReasoningEffort = "medium"
	}
}

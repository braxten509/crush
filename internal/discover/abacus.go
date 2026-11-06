package discover

import (
	"cmp"
	"encoding/json"
	"slices"
	"strconv"
	"strings"

	"charm.land/catwalk/v3/pkg/catwalk"
	"charm.land/catwalk/v3/pkg/embedded"
)

func abacusModel(raw json.RawMessage, providers ...catwalk.Provider) (catwalk.Model, bool) {
	var entry struct {
		ID                     string   `json:"id"`
		DisplayName            string   `json:"display_name"`
		ModelType              string   `json:"model_type"`
		APIFormats             []string `json:"api_formats"`
		InputModalities        []string `json:"input_modalities"`
		ContextLength          int64    `json:"context_length"`
		MaxCompletionTokens    int64    `json:"max_completion_tokens"`
		InputTokenRate         string   `json:"input_token_rate"`
		OutputTokenRate        string   `json:"output_token_rate"`
		CachedInputTokenRate   string   `json:"cached_input_token_rate"`
		Thinking               bool     `json:"thinking"`
		Tools                  bool     `json:"tools"`
		ReasoningLevels        []string `json:"reasoning_levels"`
		DefaultReasoningEffort string   `json:"default_reasoning_effort"`
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
		ID:               entry.ID,
		Name:             cmp.Or(entry.DisplayName, entry.ID),
		ContextWindow:    cmp.Or(entry.ContextLength, int64(200_000)),
		DefaultMaxTokens: min(cmp.Or(entry.MaxCompletionTokens, int64(32_000)), 128_000),
		Pricing: catwalk.Pricing{
			Input:       rate(entry.InputTokenRate),
			Output:      rate(entry.OutputTokenRate),
			CacheCreate: rate(entry.CachedInputTokenRate),
		},
		Capabilities: catwalk.Capabilities{Vision: slices.Contains(entry.InputModalities, "image")},
		Options:      catwalk.ModelOptions{ProviderOptions: map[string]any{"abacus_api_format": format}},
	}
	if entry.Thinking || strings.HasSuffix(entry.ID, "-thinking") {
		model.Reasoning.Thinking = catwalk.ThinkingToggleable
	}
	applyAbacusControls(&model, providers)
	if entry.ReasoningLevels != nil {
		model.Reasoning.EffortLevels = catwalk.NewEffortLevels(entry.ReasoningLevels...)
		model.Reasoning.DefaultEffortLevel = entry.DefaultReasoningEffort
		if len(entry.ReasoningLevels) > 0 && !model.CanReason() {
			model.Reasoning.Thinking = catwalk.ThinkingToggleable
		}
	}
	if !slices.Contains(model.ReasoningEffortLevels(), model.Reasoning.DefaultEffortLevel) {
		model.Reasoning.DefaultEffortLevel = ""
	}
	return model, true
}

// adoptAbacusControls gives an Abacus model the reasoning controls of the
// same model in a native provider's catalog. The model still reasons when
// its own listing says so, even if the catalog entry doesn't.
func adoptAbacusControls(model *catwalk.Model, known catwalk.Model) {
	thinking := model.Reasoning.Thinking
	if known.CanReason() {
		thinking = known.Reasoning.Thinking
	}
	model.Reasoning = catwalk.Reasoning{
		Thinking:           thinking,
		EffortLevels:       slices.Clone(known.Reasoning.EffortLevels),
		DefaultEffortLevel: known.Reasoning.DefaultEffortLevel,
	}
}

func applyAbacusControls(model *catwalk.Model, providers []catwalk.Provider) {
	if len(providers) == 0 {
		providers = embedded.GetAll()
	}
	// Provider namespaces and the optional thinking label do not identify a
	// different version. Never replace version numbers or infer a sibling's
	// controls (for example, GPT-6 Sol and Astra have different effort levels).
	identity := func(id string) string {
		id = strings.ToLower(id)
		if i := strings.LastIndex(id, "/"); i >= 0 {
			id = id[i+1:]
		}
		return strings.TrimSuffix(id, "-thinking")
	}
	name := func(value string) string {
		return strings.ToLower(strings.NewReplacer(" ", "", "-", "", "_", "").Replace(value))
	}
	var matches []catwalk.Model
	for _, provider := range providers {
		switch string(provider.ID) {
		case "anthropic", "openai", "gemini", "xai", "deepseek", "zai", "minimax", "moonshotai":
			for _, known := range provider.Models {
				if identity(known.ID) == identity(model.ID) {
					adoptAbacusControls(model, known)
					return
				}
				if model.Name != "" && known.Name != "" && name(known.Name) == name(model.Name) {
					matches = append(matches, known)
				}
			}
		}
	}
	if len(matches) == 1 {
		adoptAbacusControls(model, matches[0])
	}
}

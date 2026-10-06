package config

import (
	"strings"

	"charm.land/catwalk/pkg/catwalk"
)

const AbacusProviderID = "abacus"
const AbacusAPIFormatOption = "abacus_api_format"

func AbacusAPIFormat(model catwalk.Model) string {
	if format, ok := model.Options.ProviderOptions[AbacusAPIFormatOption].(string); ok {
		return format
	}
	if strings.HasPrefix(model.ID, "claude-") {
		return "anthropic"
	}
	if strings.HasPrefix(model.ID, "gpt-5") || strings.HasPrefix(model.ID, "gpt-6") || strings.HasPrefix(model.ID, "o3") || strings.HasPrefix(model.ID, "o4") {
		return "responses"
	}
	return "openai"
}

func AbacusProviderForModel(provider ProviderConfig, model catwalk.Model) ProviderConfig {
	if provider.ID != AbacusProviderID {
		return provider
	}
	switch AbacusAPIFormat(model) {
	case "anthropic":
		provider.Type = catwalk.TypeAnthropic
		provider.BaseURL = strings.TrimSuffix(strings.TrimRight(provider.BaseURL, "/"), "/v1")
	case "responses":
		provider.Type = catwalk.TypeOpenAI
	default:
		provider.Type = catwalk.TypeOpenAICompat
	}
	return provider
}

func SupportsFastMode(provider ProviderConfig, model catwalk.Model) bool {
	if provider.Type == TypeClaudeCode || provider.Type == TypeCodexCLI {
		return true
	}
	return provider.ID == AbacusProviderID && AbacusAPIFormat(model) == "responses" &&
		!strings.Contains(model.ID, "nano") && !strings.HasSuffix(model.ID, "-pro") &&
		(strings.HasPrefix(model.ID, "gpt-") || strings.HasPrefix(model.ID, "o3") || strings.HasPrefix(model.ID, "o4"))
}

// SupportsUltracode reports whether the model has Claude Code's Ultracode.
// Claude offers it on the models with effort levels; Haiku has neither.
func SupportsUltracode(provider ProviderConfig, model catwalk.Model) bool {
	return provider.Type == TypeClaudeCode && len(model.ReasoningLevels) > 0
}

func SupportsThinkingToggle(provider ProviderConfig, model catwalk.Model) bool {
	if !model.CanReason || len(model.ReasoningLevels) > 0 {
		return false
	}
	if provider.ID != AbacusProviderID {
		return true
	}
	id := strings.ToLower(model.ID)
	return AbacusAPIFormat(model) == "anthropic" || strings.Contains(id, "deepseek") || strings.HasPrefix(id, "zai-org/") || strings.Contains(id, "minimax")
}

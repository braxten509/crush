// Package providers turns Crush's provider configuration into Fantasy
// providers, language models and per-call provider options.
package providers

import (
	"bytes"
	"cmp"
	"encoding/json"
	"io"
	"log/slog"
	"maps"
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
	"github.com/charmbracelet/crush/internal/agent/hyper"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/discover"
	"github.com/qjebbs/go-jsons"
)

// modelConfig is the catalog entry and the user's selection of a model.
type modelConfig struct {
	CatwalkCfg catwalk.Model
	ModelCfg   config.SelectedModel
}

// effectiveReasoningEffort returns the reasoning effort to apply for provider calls.
// It prefers the user-selected effort when valid, otherwise the model default when
// valid, and finally falls back to the first configured reasoning level.
func effectiveReasoningEffort(model modelConfig) string {
	if !model.CatwalkCfg.CanReason {
		return ""
	}

	if effort := model.ModelCfg.ReasoningEffort; effort != "" && slices.Contains(model.CatwalkCfg.ReasoningLevels, effort) {
		return effort
	}
	if effort := model.CatwalkCfg.DefaultReasoningEffort; effort != "" && slices.Contains(model.CatwalkCfg.ReasoningLevels, effort) {
		return effort
	}
	if len(model.CatwalkCfg.ReasoningLevels) > 0 {
		return model.CatwalkCfg.ReasoningLevels[0]
	}
	return ""
}

// Options returns the provider options for a call to the selected model.
func Options(catwalkModel catwalk.Model, selected config.SelectedModel, providerCfg config.ProviderConfig) fantasy.ProviderOptions {
	model := modelConfig{CatwalkCfg: catwalkModel, ModelCfg: selected}
	providerCfg = config.AbacusProviderForModel(providerCfg, model.CatwalkCfg)
	options := fantasy.ProviderOptions{}

	cfgOpts := []byte("{}")
	providerCfgOpts := []byte("{}")
	catwalkOpts := []byte("{}")

	if model.ModelCfg.ProviderOptions != nil {
		data, err := json.Marshal(model.ModelCfg.ProviderOptions)
		if err == nil {
			cfgOpts = data
		}
	}

	if providerCfg.ProviderOptions != nil {
		data, err := json.Marshal(providerCfg.ProviderOptions)
		if err == nil {
			providerCfgOpts = data
		}
	}

	if model.CatwalkCfg.Options.ProviderOptions != nil {
		data, err := json.Marshal(model.CatwalkCfg.Options.ProviderOptions)
		if err == nil {
			catwalkOpts = data
		}
	}

	readers := []io.Reader{
		bytes.NewReader(catwalkOpts),
		bytes.NewReader(providerCfgOpts),
		bytes.NewReader(cfgOpts),
	}

	got, err := jsons.Merge(readers)
	if err != nil {
		slog.Error("Could not merge call config", "err", err)
		return options
	}

	mergedOptions := make(map[string]any)

	err = json.Unmarshal([]byte(got), &mergedOptions)
	if err != nil {
		slog.Error("Could not create config for call", "err", err)
		return options
	}
	delete(mergedOptions, config.AbacusAPIFormatOption)
	if providerCfg.ID == config.AbacusProviderID && len(providerCfg.ExtraBody) > 0 {
		extra, _ := mergedOptions["extra_body"].(map[string]any)
		extra = maps.Clone(extra)
		if extra == nil {
			extra = make(map[string]any)
		}
		for key, value := range providerCfg.ExtraBody {
			if _, exists := extra[key]; !exists {
				extra[key] = value
			}
		}
		mergedOptions["extra_body"] = extra
	}

	reasoningEffort := effectiveReasoningEffort(model)
	shouldSetEffort := model.CatwalkCfg.CanReason &&
		reasoningEffort != "" &&
		slices.Contains(model.CatwalkCfg.ReasoningLevels, reasoningEffort)

	switch providerCfg.Type {
	case openai.Name, azure.Name:
		if providerCfg.ID == config.AbacusProviderID && config.SupportsFastMode(providerCfg, model.CatwalkCfg) && model.ModelCfg.ServiceTier != "" {
			if model.ModelCfg.ServiceTier == "fast" {
				mergedOptions["service_tier"] = "priority"
			} else {
				mergedOptions["service_tier"] = "auto"
			}
		}
		_, hasReasoningEffort := mergedOptions["reasoning_effort"]
		if !hasReasoningEffort && shouldSetEffort {
			mergedOptions["reasoning_effort"] = reasoningEffort
		}
		if openai.IsResponsesModel(model.CatwalkCfg.ID) || (providerCfg.ID == config.AbacusProviderID && config.AbacusAPIFormat(model.CatwalkCfg) == "responses") {
			if openai.IsResponsesReasoningModel(model.CatwalkCfg.ID) {
				mergedOptions["reasoning_summary"] = "auto"
				mergedOptions["include"] = []openai.IncludeType{openai.IncludeReasoningEncryptedContent}
			}
			parsed, err := openai.ParseResponsesOptions(mergedOptions)
			if err == nil {
				options[openai.Name] = parsed
			}
		} else {
			parsed, err := openai.ParseOptions(mergedOptions)
			if err == nil {
				options[openai.Name] = parsed
			}
		}

	case anthropic.Name, bedrock.Name:
		var (
			_, hasEffort = mergedOptions["effort"]
			_, hasThink  = mergedOptions["thinking"]
			extraBody    = make(map[string]any)
		)

		switch providerCfg.ID {
		case string(catwalk.InferenceProviderAlibabaSingapore), string(catwalk.InferenceProviderAlibabaUS):
			switch {
			case !hasEffort && shouldSetEffort:
				extraBody["reasoning_effort"] = reasoningEffort
			case !hasThink && model.CatwalkCfg.CanReason:
				if model.ModelCfg.Think {
					extraBody["thinking"] = map[string]any{"type": "enabled"}
				} else {
					extraBody["thinking"] = map[string]any{"type": "disabled"}
				}
			}
			mergedOptions["extra_body"] = extraBody

		default:
			switch {
			case !hasEffort && shouldSetEffort:
				mergedOptions["effort"] = reasoningEffort
			case !hasThink && model.ModelCfg.Think:
				mergedOptions["thinking"] = map[string]any{"budget_tokens": 2000}
			}
		}

		parsed, err := anthropic.ParseOptions(mergedOptions)
		if err == nil {
			options[anthropic.Name] = parsed
		}

	case openrouter.Name:
		_, hasReasoning := mergedOptions["reasoning"]
		if !hasReasoning && shouldSetEffort {
			mergedOptions["reasoning"] = map[string]any{
				"enabled": true,
				"effort":  reasoningEffort,
			}
		}
		parsed, err := openrouter.ParseOptions(mergedOptions)
		if err == nil {
			options[openrouter.Name] = parsed
		}

	case vercel.Name:
		_, hasReasoning := mergedOptions["reasoning"]
		if !hasReasoning && shouldSetEffort {
			mergedOptions["reasoning"] = map[string]any{
				"enabled": true,
				"effort":  reasoningEffort,
			}
		}
		parsed, err := vercel.ParseOptions(mergedOptions)
		if err == nil {
			options[vercel.Name] = parsed
		}

	case google.Name:
		_, hasReasoning := mergedOptions["thinking_config"]
		if !hasReasoning {
			if strings.HasPrefix(model.CatwalkCfg.ID, "gemini-2") {
				mergedOptions["thinking_config"] = map[string]any{
					"thinking_budget":  2000,
					"include_thoughts": true,
				}
			} else {
				mergedOptions["thinking_config"] = map[string]any{
					"thinking_level":   reasoningEffort,
					"include_thoughts": true,
				}
			}
		}
		parsed, err := google.ParseOptions(mergedOptions)
		if err == nil {
			options[google.Name] = parsed
		}

	case openaicompat.Name, hyper.Name:
		extraBody := make(map[string]any)

		_, hasReasoningEffort := mergedOptions["reasoning_effort"]
		if !hasReasoningEffort && shouldSetEffort {
			switch providerCfg.ID {
			case string(catwalk.InferenceProviderIoNet):
				extraBody["reasoning"] = map[string]string{"effort": reasoningEffort}
			case string(catwalk.InferenceProviderOpenCodeGo), string(catwalk.InferenceProviderOpenCodeZen):
				// MiniMax models use the "thinking" parameter instead of
				// "reasoning_effort". Other models on these providers still
				// use the standard field.
				if !strings.HasPrefix(strings.ToLower(model.CatwalkCfg.ID), "minimax") {
					mergedOptions["reasoning_effort"] = reasoningEffort
				}
			default:
				mergedOptions["reasoning_effort"] = reasoningEffort
			}
		}

		// "reasoning effort" is a standard OpenAI field, but "thinking" is not.
		// Setting it in the right way for each provider.
		// TODO: Abstract this in Fantasy somehow?
		// TODO: Allow custom providers to specify how to set this?
		switch providerCfg.ID {
		case config.AbacusProviderID:
			if existing, ok := mergedOptions["extra_body"].(map[string]any); ok {
				for key, value := range existing {
					extraBody[key] = value
				}
			}
			id := strings.ToLower(model.CatwalkCfg.ID)
			if _, explicit := extraBody["thinking"]; explicit {
				break
			}
			switch {
			case strings.Contains(id, "minimax"):
				if model.ModelCfg.Think || shouldSetEffort {
					mode := "enabled"
					if strings.Contains(id, "minimax-m3") {
						mode = "adaptive"
						extraBody["reasoning_split"] = true
					}
					extraBody["thinking"] = map[string]any{"type": mode}
				} else {
					extraBody["thinking"] = map[string]any{"type": "disabled"}
				}
				delete(mergedOptions, "reasoning_effort")
			case strings.Contains(id, "deepseek") || strings.HasPrefix(id, "zai-org/"):
				if model.ModelCfg.Think || (shouldSetEffort && reasoningEffort != "none") {
					extraBody["thinking"] = map[string]any{"type": "enabled"}
				} else {
					extraBody["thinking"] = map[string]any{"type": "disabled"}
				}
			}
		case hyper.Name:
			extraBody["thinking"] = model.ModelCfg.Think
		case string(catwalk.InferenceProviderIoNet):
			if _, ok := extraBody["reasoning"]; !ok && model.CatwalkCfg.CanReason {
				if model.ModelCfg.Think {
					extraBody["reasoning"] = map[string]string{"effort": "medium"}
				} else {
					extraBody["reasoning"] = map[string]string{"effort": "none"}
				}
			}

		case string(catwalk.InferenceProviderZAI), string(catwalk.InferenceProviderDeepSeek):
			if model.ModelCfg.Think || reasoningEffort != "" {
				extraBody["thinking"] = map[string]any{"type": "enabled"}
			} else {
				extraBody["thinking"] = map[string]any{"type": "disabled"}
			}

		case string(catwalk.InferenceProviderFireworks):
			// NOTE: Fireworks break if we set both `reasoning_effort` and `thinking`.
			if reasoningEffort == "" {
				if model.ModelCfg.Think {
					extraBody["thinking"] = map[string]any{"type": "enabled"}
				} else {
					extraBody["thinking"] = map[string]any{"type": "disabled"}
				}
			}

		case string(catwalk.InferenceProviderBaseten):
			extraBody["chat_template_args"] = map[string]any{
				"enable_thinking": model.ModelCfg.Think || reasoningEffort != "" && reasoningEffort != "none",
			}

		case string(catwalk.InferenceProviderOpenCodeGo), string(catwalk.InferenceProviderOpenCodeZen):
			// MiniMax M3 uses the "thinking" parameter to control reasoning.
			// "reasoning_split" must be true so thinking content is returned
			// in the "reasoning_content" field instead of inline in "content".
			if strings.HasPrefix(strings.ToLower(model.CatwalkCfg.ID), "minimax") {
				if model.CatwalkCfg.CanReason && (model.ModelCfg.Think || reasoningEffort != "") {
					extraBody["thinking"] = map[string]any{"type": "adaptive"}
					extraBody["reasoning_split"] = true
				} else {
					extraBody["thinking"] = map[string]any{"type": "disabled"}
				}
			}

		case string(catwalk.InferenceProviderAlibabaSingapore), string(catwalk.InferenceProviderAlibabaUS):
			if model.CatwalkCfg.CanReason && !shouldSetEffort {
				extraBody["enable_thinking"] = model.ModelCfg.Think
			}
		}

		mergedOptions["extra_body"] = extraBody

		parsed, err := openaicompat.ParseOptions(mergedOptions)
		if err == nil {
			options[openaicompat.Name] = parsed
		}

	default:
		// Known custom providers (litellm, llamacpp, lmstudio, ollama,
		// omlx) are openai-compat under the hood.
		if discover.IsKnownCustomProvider(string(providerCfg.Type)) {
			// Set "top_k" under "extra_body", as it is not part of the OpenAI protocol
			// and will be explicitly omitted by Fantasy downstream.
			topK := cmp.Or(model.ModelCfg.TopK, model.CatwalkCfg.Options.TopK)
			if topK != nil {
				extraBody, hasExtraBody := mergedOptions["extra_body"].(map[string]any)
				if !hasExtraBody {
					extraBody = make(map[string]any)
					mergedOptions["extra_body"] = extraBody
				}
				if _, hasTopK := extraBody["top_k"]; !hasTopK {
					extraBody["top_k"] = *topK
				}
			}

			_, hasReasoningEffort := mergedOptions["reasoning_effort"]
			if !hasReasoningEffort && shouldSetEffort {
				mergedOptions["reasoning_effort"] = reasoningEffort
			}

			parsed, err := openaicompat.ParseOptions(mergedOptions)
			if err == nil {
				options[openaicompat.Name] = parsed
			} else {
				if topK != nil {
					slog.Warn(
						"Failed to parse provider_options, falling back to top_k only",
						"provider", providerCfg.ID,
						"error", err,
					)

					fallbackMergeOptions := map[string]any{
						"extra_body": map[string]any{"top_k": *topK},
					}
					parsed, err := openaicompat.ParseOptions(fallbackMergeOptions)
					if err == nil {
						options[openaicompat.Name] = parsed
					} else {
						slog.Warn(
							"Failed to parse fallback provider options, this should never happen",
							"provider", providerCfg.ID,
							"error", err,
						)
					}
				}
			}
		}
	}

	return options
}

// CallTopK returns topK for use on fantasy.Call.TopK, suppressing it for
// known custom providers: Options already carries top_k for
// them via extra_body, and passing it here too makes Fantasy emit a
// spurious "top_k unsupported" warning for every turn.
func CallTopK(providerCfg config.ProviderConfig, topK *int64) *int64 {
	if discover.IsKnownCustomProvider(string(providerCfg.Type)) {
		return nil
	}
	return topK
}

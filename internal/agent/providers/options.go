// Package providers turns Crush's provider configuration into Fantasy
// providers, language models and per-call provider options.
package providers

import (
	"bytes"
	"cmp"
	"context"
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

// optionsCall is what a provider's options builder works from: the model,
// the provider, and the options merged from the catalog, the provider and
// the user's selection, which the builder completes and parses.
type optionsCall struct {
	model    modelConfig
	provider config.ProviderConfig
	merged   map[string]any
	// effort is the reasoning effort the call should use, and setEffort
	// whether it is one the model takes.
	effort    string
	setEffort bool
}

// setEffortDefault sets key in the merged options to value when the call
// should carry a reasoning effort and the user didn't configure key
// themselves. It reports whether it set it.
func (c *optionsCall) setEffortDefault(key string, value any) bool {
	if _, ok := c.merged[key]; ok || !c.setEffort {
		return false
	}
	c.merged[key] = value
	return true
}

// thinkingSwitch is the {"type": "enabled"|"disabled"} object several
// OpenAI-compatible providers use to turn thinking on and off.
func thinkingSwitch(on bool) map[string]any {
	if on {
		return map[string]any{"type": "enabled"}
	}
	return map[string]any{"type": "disabled"}
}

// optionsBuilder completes the merged options with the reasoning controls
// a provider type expects and parses them into its typed options.
type optionsBuilder func(c *optionsCall) fantasy.ProviderOptions

// optionsBuilders maps each provider type to its options builder. Types
// without one get the known custom providers' handling, or no options.
var optionsBuilders = map[catwalk.Type]optionsBuilder{
	openai.Name:       openaiOptions,
	azure.Name:        openaiOptions,
	anthropic.Name:    anthropicOptions,
	bedrock.Name:      anthropicOptions,
	openrouter.Name:   openrouterOptions,
	vercel.Name:       reasoningObjectOptions(vercel.Name, vercel.ParseOptions),
	google.Name:       googleOptions,
	openaicompat.Name: compatOptions,
	hyper.Name:        compatOptions,
}

// Options returns the provider options for a call to the selected model.
func Options(catwalkModel catwalk.Model, selected config.SelectedModel, providerCfg config.ProviderConfig) fantasy.ProviderOptions {
	model := modelConfig{CatwalkCfg: catwalkModel, ModelCfg: selected}
	providerCfg = config.AbacusProviderForModel(providerCfg, model.CatwalkCfg)
	merged, ok := mergeOptions(model, providerCfg)
	if !ok {
		return fantasy.ProviderOptions{}
	}

	effort := effectiveReasoningEffort(model)
	c := &optionsCall{
		model:    model,
		provider: providerCfg,
		merged:   merged,
		effort:   effort,
		setEffort: model.CatwalkCfg.CanReason &&
			effort != "" &&
			slices.Contains(model.CatwalkCfg.ReasoningLevels, effort),
	}
	if build, ok := optionsBuilders[providerCfg.Type]; ok {
		return build(c)
	}
	// Known custom providers (litellm, llamacpp, lmstudio, ollama, omlx)
	// are openai-compat under the hood.
	if discover.IsKnownCustomProvider(string(providerCfg.Type)) {
		return customOptions(c)
	}
	return fantasy.ProviderOptions{}
}

// mergeOptions merges the catalog's, the provider's and the user's
// provider options, later ones winning. It reports false when they can't
// be merged.
func mergeOptions(model modelConfig, providerCfg config.ProviderConfig) (map[string]any, bool) {
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
		return nil, false
	}

	mergedOptions := make(map[string]any)

	err = json.Unmarshal([]byte(got), &mergedOptions)
	if err != nil {
		slog.Error("Could not create config for call", "err", err)
		return nil, false
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
	return mergedOptions, true
}

// openaiOptions builds options for OpenAI and Azure, on the Responses API
// when the model uses it.
func openaiOptions(c *optionsCall) fantasy.ProviderOptions {
	model := c.model
	isAbacus := c.provider.ID == config.AbacusProviderID
	if isAbacus && config.SupportsFastMode(c.provider, model.CatwalkCfg) && model.ModelCfg.ServiceTier != "" {
		if model.ModelCfg.ServiceTier == "fast" {
			c.merged["service_tier"] = "priority"
		} else {
			c.merged["service_tier"] = "auto"
		}
	}
	c.setEffortDefault("reasoning_effort", c.effort)

	options := fantasy.ProviderOptions{}
	if openai.IsResponsesModel(model.CatwalkCfg.ID) || (isAbacus && config.AbacusAPIFormat(model.CatwalkCfg) == "responses") {
		if openai.IsResponsesReasoningModel(model.CatwalkCfg.ID) {
			c.merged["reasoning_summary"] = "auto"
			c.merged["include"] = []openai.IncludeType{openai.IncludeReasoningEncryptedContent}
		}
		if parsed, err := openai.ParseResponsesOptions(c.merged); err == nil {
			options[openai.Name] = parsed
		}
	} else if parsed, err := openai.ParseOptions(c.merged); err == nil {
		options[openai.Name] = parsed
	}
	return options
}

// anthropicQuirks are provider IDs that serve the Anthropic API but take
// their reasoning controls differently.
var anthropicQuirks = map[string]func(c *optionsCall, hasEffort, hasThink bool){
	string(catwalk.InferenceProviderAlibabaSingapore): alibabaAnthropicReasoning,
	string(catwalk.InferenceProviderAlibabaUS):        alibabaAnthropicReasoning,
}

// anthropicOptions builds options for Anthropic and Bedrock, which is
// Anthropic under another provider name: its options must land under
// anthropic.Name for the Anthropic language model to pick them up.
func anthropicOptions(c *optionsCall) fantasy.ProviderOptions {
	_, hasEffort := c.merged["effort"]
	_, hasThink := c.merged["thinking"]
	if quirk, ok := anthropicQuirks[c.provider.ID]; ok {
		quirk(c, hasEffort, hasThink)
	} else if !c.setEffortDefault("effort", c.effort) && !hasThink && c.model.ModelCfg.Think {
		c.merged["thinking"] = map[string]any{"budget_tokens": 2000}
	}

	options := fantasy.ProviderOptions{}
	if parsed, err := anthropic.ParseOptions(c.merged); err == nil {
		options[anthropic.Name] = parsed
	}
	return options
}

// alibabaAnthropicReasoning passes the effort, or else the thinking switch,
// in the request body.
func alibabaAnthropicReasoning(c *optionsCall, hasEffort, hasThink bool) {
	extraBody := make(map[string]any)
	switch {
	case !hasEffort && c.setEffort:
		extraBody["reasoning_effort"] = c.effort
	case !hasThink && c.model.CatwalkCfg.CanReason:
		extraBody["thinking"] = thinkingSwitch(c.model.ModelCfg.Think)
	}
	c.merged["extra_body"] = extraBody
}

// reasoningObjectOptions builds options for providers that take the effort
// in a "reasoning" object (OpenRouter and Vercel).
func reasoningObjectOptions[T fantasy.ProviderOptionsData](name string, parse func(map[string]any) (T, error)) optionsBuilder {
	return func(c *optionsCall) fantasy.ProviderOptions {
		c.setEffortDefault("reasoning", map[string]any{
			"enabled": true,
			"effort":  c.effort,
		})
		options := fantasy.ProviderOptions{}
		if parsed, err := parse(c.merged); err == nil {
			options[name] = parsed
		}
		return options
	}
}

// openRouterHostOrder reads a model's OpenRouter hosts, cheapest first;
// tests replace it.
var openRouterHostOrder = discover.OpenRouterHostOrder

// openRouterQuantizations is the quality floor every OpenRouter request
// carries: FP8 and better, and hosts that do not say their precision.
// Anything compressed further (fp6, fp4, int4) never answers (owner,
// 2026-10-22).
var openRouterQuantizations = []string{"fp8", "mxfp8", "fp16", "bf16", "fp32", "unknown"}

// openrouterOptions asks OpenRouter for the cheapest host first (provider
// sort "price"; owner, 2026-10-22) unless the provider's switch is off or
// the user's own options already pick a sort or an order, and refuses hosts
// compressed below FP8 unless that switch is off or the user names their own
// precisions. Both share one provider object. The account's own provider and
// privacy settings still apply on OpenRouter's side.
func openrouterOptions(c *optionsCall) fantasy.ProviderOptions {
	routing, _ := c.merged["provider"].(map[string]any)
	floor := c.provider.QualityFloorOn() && routing["quantizations"] == nil
	cheapest := c.provider.CheapestHostOn() && routing["sort"] == nil && routing["order"] == nil
	if floor || cheapest {
		next := maps.Clone(routing)
		if next == nil {
			next = map[string]any{}
		}
		if floor {
			next["quantizations"] = slices.Clone(openRouterQuantizations)
		}
		if cheapest {
			next["sort"] = "price"
			// Once the model's hosts are read, they go first by price with
			// cache reads breaking ties (discover.OpenRouterHostOrder).
			if order := openRouterHostOrder(context.Background(), c.provider.BaseURL, c.model.CatwalkCfg.ID); len(order) > 0 {
				next["order"] = order
			}
		}
		c.merged["provider"] = next
	}
	return reasoningObjectOptions(openrouter.Name, openrouter.ParseOptions)(c)
}

// googleOptions builds options for Gemini: a thinking budget for Gemini 2
// models, and a thinking level for later ones.
func googleOptions(c *optionsCall) fantasy.ProviderOptions {
	if _, hasReasoning := c.merged["thinking_config"]; !hasReasoning {
		if strings.HasPrefix(c.model.CatwalkCfg.ID, "gemini-2") {
			c.merged["thinking_config"] = map[string]any{
				"thinking_budget":  2000,
				"include_thoughts": true,
			}
		} else {
			c.merged["thinking_config"] = map[string]any{
				"thinking_level":   c.effort,
				"include_thoughts": true,
			}
		}
	}
	options := fantasy.ProviderOptions{}
	if parsed, err := google.ParseOptions(c.merged); err == nil {
		options[google.Name] = parsed
	}
	return options
}

// compatQuirk adapts the OpenAI-compatible request for one provider ID.
// "reasoning_effort" is a standard OpenAI field, but "thinking" is not, so
// each provider that has one sets it its own way.
//
// TODO: Abstract this in Fantasy somehow?
// TODO: Allow custom providers to specify how to set this?
type compatQuirk struct {
	// effort places the reasoning effort when the call carries one the
	// user didn't configure. Nil sets "reasoning_effort".
	effort func(c *optionsCall, extraBody map[string]any)
	// thinking sets the provider's own reasoning switches in the body.
	thinking func(c *optionsCall, extraBody map[string]any)
}

// compatQuirks are the OpenAI-compatible providers with their own
// reasoning controls.
var compatQuirks = map[string]compatQuirk{
	config.IFMProviderID: {effort: func(c *optionsCall, extraBody map[string]any) {
		if existing, ok := c.merged["extra_body"].(map[string]any); ok {
			maps.Copy(extraBody, existing)
		}
		kwargs, _ := extraBody["chat_template_kwargs"].(map[string]any)
		kwargs = maps.Clone(kwargs)
		if kwargs == nil {
			kwargs = make(map[string]any)
		}
		if _, explicit := kwargs["reasoning_effort"]; !explicit {
			kwargs["reasoning_effort"] = c.effort
		}
		extraBody["chat_template_kwargs"] = kwargs
	}},
	config.AbacusProviderID: {thinking: abacusThinking},
	hyper.Name: {thinking: func(c *optionsCall, extraBody map[string]any) {
		extraBody["thinking"] = c.model.ModelCfg.Think
	}},
	string(catwalk.InferenceProviderIoNet): {
		effort: func(c *optionsCall, extraBody map[string]any) {
			extraBody["reasoning"] = map[string]string{"effort": c.effort}
		},
		thinking: func(c *optionsCall, extraBody map[string]any) {
			if _, ok := extraBody["reasoning"]; !ok && c.model.CatwalkCfg.CanReason {
				if c.model.ModelCfg.Think {
					extraBody["reasoning"] = map[string]string{"effort": "medium"}
				} else {
					extraBody["reasoning"] = map[string]string{"effort": "none"}
				}
			}
		},
	},
	string(catwalk.InferenceProviderZAI):       {thinking: enabledWhenReasoning},
	string(catwalk.InferenceProviderDeepSeek):  {thinking: enabledWhenReasoning},
	string(catwalk.InferenceProviderFireworks): {thinking: fireworksThinking},
	string(catwalk.InferenceProviderBaseten): {thinking: func(c *optionsCall, extraBody map[string]any) {
		extraBody["chat_template_args"] = map[string]any{
			"enable_thinking": c.model.ModelCfg.Think || c.effort != "" && c.effort != "none",
		}
	}},
	string(catwalk.InferenceProviderOpenCodeGo):       {effort: openCodeEffort, thinking: openCodeThinking},
	string(catwalk.InferenceProviderOpenCodeZen):      {effort: openCodeEffort, thinking: openCodeThinking},
	string(catwalk.InferenceProviderAlibabaSingapore): {thinking: alibabaCompatThinking},
	string(catwalk.InferenceProviderAlibabaUS):        {thinking: alibabaCompatThinking},
}

// compatOptions builds options for OpenAI-compatible providers and Hyper.
func compatOptions(c *optionsCall) fantasy.ProviderOptions {
	extraBody := make(map[string]any)
	quirk := compatQuirks[c.provider.ID]

	if _, ok := c.merged["reasoning_effort"]; !ok && c.setEffort {
		if quirk.effort != nil {
			quirk.effort(c, extraBody)
		} else {
			c.merged["reasoning_effort"] = c.effort
		}
	}
	if quirk.thinking != nil {
		quirk.thinking(c, extraBody)
	}
	c.merged["extra_body"] = extraBody

	options := fantasy.ProviderOptions{}
	if parsed, err := openaicompat.ParseOptions(c.merged); err == nil {
		options[openaicompat.Name] = parsed
	}
	return options
}

// abacusThinking keeps the user's request body and sets the thinking
// switch the model's family expects, unless the user set one.
func abacusThinking(c *optionsCall, extraBody map[string]any) {
	if existing, ok := c.merged["extra_body"].(map[string]any); ok {
		maps.Copy(extraBody, existing)
	}
	if _, explicit := extraBody["thinking"]; explicit {
		return
	}
	id := strings.ToLower(c.model.CatwalkCfg.ID)
	switch {
	case strings.Contains(id, "minimax"):
		if c.model.ModelCfg.Think || c.setEffort {
			mode := "enabled"
			if strings.Contains(id, "minimax-m3") {
				mode = "adaptive"
				extraBody["reasoning_split"] = true
			}
			extraBody["thinking"] = map[string]any{"type": mode}
		} else {
			extraBody["thinking"] = thinkingSwitch(false)
		}
		delete(c.merged, "reasoning_effort")
	case strings.Contains(id, "deepseek") || strings.HasPrefix(id, "zai-org/"):
		extraBody["thinking"] = thinkingSwitch(c.model.ModelCfg.Think || (c.setEffort && c.effort != "none"))
	}
}

// enabledWhenReasoning turns thinking on whenever the call reasons.
func enabledWhenReasoning(c *optionsCall, extraBody map[string]any) {
	extraBody["thinking"] = thinkingSwitch(c.model.ModelCfg.Think || c.effort != "")
}

// fireworksThinking sets the thinking switch only without an effort:
// Fireworks breaks if both `reasoning_effort` and `thinking` are set.
func fireworksThinking(c *optionsCall, extraBody map[string]any) {
	if c.effort == "" {
		extraBody["thinking"] = thinkingSwitch(c.model.ModelCfg.Think)
	}
}

// openCodeEffort sets "reasoning_effort" except for MiniMax models, which
// use the "thinking" parameter instead. Other models on these providers
// still use the standard field.
func openCodeEffort(c *optionsCall, _ map[string]any) {
	if !isMiniMax(c.model.CatwalkCfg.ID) {
		c.merged["reasoning_effort"] = c.effort
	}
}

// openCodeThinking controls MiniMax M3's reasoning with the "thinking"
// parameter. "reasoning_split" must be true so thinking content is
// returned in the "reasoning_content" field instead of inline in
// "content".
func openCodeThinking(c *optionsCall, extraBody map[string]any) {
	if !isMiniMax(c.model.CatwalkCfg.ID) {
		return
	}
	if c.model.CatwalkCfg.CanReason && (c.model.ModelCfg.Think || c.effort != "") {
		extraBody["thinking"] = map[string]any{"type": "adaptive"}
		extraBody["reasoning_split"] = true
	} else {
		extraBody["thinking"] = thinkingSwitch(false)
	}
}

func isMiniMax(modelID string) bool {
	return strings.HasPrefix(strings.ToLower(modelID), "minimax")
}

// alibabaCompatThinking switches thinking for models that reason without
// effort levels.
func alibabaCompatThinking(c *optionsCall, extraBody map[string]any) {
	if c.model.CatwalkCfg.CanReason && !c.setEffort {
		extraBody["enable_thinking"] = c.model.ModelCfg.Think
	}
}

// customOptions builds options for the known custom providers, which speak
// OpenAI-compat.
func customOptions(c *optionsCall) fantasy.ProviderOptions {
	// Set "top_k" under "extra_body", as it is not part of the OpenAI protocol
	// and will be explicitly omitted by Fantasy downstream.
	topK := cmp.Or(c.model.ModelCfg.TopK, c.model.CatwalkCfg.Options.TopK)
	if topK != nil {
		extraBody, hasExtraBody := c.merged["extra_body"].(map[string]any)
		if !hasExtraBody {
			extraBody = make(map[string]any)
			c.merged["extra_body"] = extraBody
		}
		if _, hasTopK := extraBody["top_k"]; !hasTopK {
			extraBody["top_k"] = *topK
		}
	}

	c.setEffortDefault("reasoning_effort", c.effort)

	options := fantasy.ProviderOptions{}
	parsed, err := openaicompat.ParseOptions(c.merged)
	if err == nil {
		options[openaicompat.Name] = parsed
		return options
	}
	if topK == nil {
		return options
	}
	slog.Warn(
		"Failed to parse provider_options, falling back to top_k only",
		"provider", c.provider.ID,
		"error", err,
	)
	fallbackMergeOptions := map[string]any{
		"extra_body": map[string]any{"top_k": *topK},
	}
	parsed, err = openaicompat.ParseOptions(fallbackMergeOptions)
	if err == nil {
		options[openaicompat.Name] = parsed
	} else {
		slog.Warn(
			"Failed to parse fallback provider options, this should never happen",
			"provider", c.provider.ID,
			"error", err,
		)
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

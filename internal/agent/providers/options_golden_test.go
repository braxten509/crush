package providers

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"charm.land/catwalk/pkg/catwalk"
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
	"github.com/stretchr/testify/require"
)

var updateProviderOptions = flag.Bool("update-provider-options", false, "rewrite testdata/provider_options.golden")

// providerOptionsProviders are the provider configurations the
// characterization covers: every provider type, and every provider ID that
// has its own quirk.
func providerOptionsProviders() []config.ProviderConfig {
	providers := []config.ProviderConfig{
		{ID: "openai", Type: openai.Name},
		{ID: "azure", Type: azure.Name},
		{ID: config.AbacusProviderID, Type: openaicompat.Name},
		{ID: config.AbacusProviderID, Type: openaicompat.Name, ExtraBody: map[string]any{"route": "fast", "custom": "provider"}},
		{ID: "anthropic", Type: anthropic.Name},
		{ID: string(catwalk.InferenceProviderMiniMax), Type: anthropic.Name},
		{ID: string(catwalk.InferenceProviderAlibabaSingapore), Type: anthropic.Name},
		{ID: string(catwalk.InferenceProviderAlibabaUS), Type: anthropic.Name},
		{ID: string(catwalk.InferenceProviderOpenCodeZen), Type: anthropic.Name},
		{ID: "bedrock", Type: bedrock.Name},
		{ID: string(catwalk.InferenceProviderBedrockEurope), Type: bedrock.Name},
		{ID: "openrouter", Type: openrouter.Name},
		{ID: "vercel", Type: vercel.Name},
		{ID: "gemini", Type: google.Name},
		{ID: "vertexai", Type: catwalk.TypeVertexAI},
		{ID: "custom-compat", Type: openaicompat.Name},
		{ID: "custom-compat-options", Type: openaicompat.Name, ProviderOptions: map[string]any{"user": "provider-user", "extra_body": map[string]any{"from": "provider"}}},
		{ID: string(catwalk.InferenceProviderIoNet), Type: openaicompat.Name},
		{ID: string(catwalk.InferenceProviderOpenCodeGo), Type: openaicompat.Name},
		{ID: string(catwalk.InferenceProviderOpenCodeZen), Type: openaicompat.Name},
		{ID: string(catwalk.InferenceProviderZAI), Type: openaicompat.Name},
		{ID: string(catwalk.InferenceProviderDeepSeek), Type: openaicompat.Name},
		{ID: string(catwalk.InferenceProviderFireworks), Type: openaicompat.Name},
		{ID: string(catwalk.InferenceProviderBaseten), Type: openaicompat.Name},
		{ID: string(catwalk.InferenceProviderAlibabaSingapore), Type: openaicompat.Name},
		{ID: string(catwalk.InferenceProviderAlibabaUS), Type: openaicompat.Name},
		{ID: string(catwalk.InferenceProviderCopilot), Type: openaicompat.Name},
		{ID: hyper.Name, Type: hyper.Name},
		{ID: hyper.Name, Type: openaicompat.Name},
		{ID: "claude-code", Type: config.TypeClaudeCode},
		{ID: "codex", Type: config.TypeCodexCLI},
		{ID: "unknown", Type: "made-up-type"},
	}
	for _, providerType := range discover.RegisteredProviderTypes() {
		providers = append(providers, config.ProviderConfig{ID: providerType, Type: catwalk.Type(providerType)})
	}
	return providers
}

// providerOptionsModels are the model selections the characterization
// covers: reasoning on and off, every way an effort is picked, explicit
// options that must win, and the model IDs some providers special-case.
func providerOptionsModels() []struct {
	name  string
	model modelConfig
} {
	levels := []string{"low", "medium", "high"}
	topK := int64(40)
	catwalkTopK := int64(64)
	return []struct {
		name  string
		model modelConfig
	}{
		{"plain", modelConfig{CatwalkCfg: catwalk.Model{ID: "plain-model"}}},
		{"reasoning-default", modelConfig{CatwalkCfg: catwalk.Model{ID: "reasoner", CanReason: true, ReasoningLevels: levels, DefaultReasoningEffort: "medium"}}},
		{"reasoning-selected", modelConfig{CatwalkCfg: catwalk.Model{ID: "reasoner", CanReason: true, ReasoningLevels: levels, DefaultReasoningEffort: "medium"}, ModelCfg: config.SelectedModel{ReasoningEffort: "high"}}},
		{"reasoning-invalid", modelConfig{CatwalkCfg: catwalk.Model{ID: "reasoner", CanReason: true, ReasoningLevels: levels, DefaultReasoningEffort: "ultra"}, ModelCfg: config.SelectedModel{ReasoningEffort: "extreme"}}},
		{"reasoning-none", modelConfig{CatwalkCfg: catwalk.Model{ID: "reasoner", CanReason: true, ReasoningLevels: []string{"none", "low", "high"}}, ModelCfg: config.SelectedModel{ReasoningEffort: "none"}}},
		{"think", modelConfig{CatwalkCfg: catwalk.Model{ID: "thinker", CanReason: true}, ModelCfg: config.SelectedModel{Think: true}}},
		{"think-off", modelConfig{CatwalkCfg: catwalk.Model{ID: "thinker", CanReason: true}}},
		{"think-cannot-reason", modelConfig{CatwalkCfg: catwalk.Model{ID: "thinker"}, ModelCfg: config.SelectedModel{Think: true}}},
		{"explicit-options", modelConfig{
			CatwalkCfg: catwalk.Model{ID: "reasoner", CanReason: true, ReasoningLevels: levels},
			ModelCfg: config.SelectedModel{ReasoningEffort: "high", Think: true, ProviderOptions: map[string]any{
				"reasoning_effort": "low",
				"effort":           "low",
				"reasoning":        map[string]any{"enabled": false},
				"thinking_config":  map[string]any{"thinking_budget": 10},
			}},
		}},
		{"explicit-thinking", modelConfig{
			CatwalkCfg: catwalk.Model{ID: "reasoner", CanReason: true},
			ModelCfg:   config.SelectedModel{Think: true, ProviderOptions: map[string]any{"thinking": map[string]any{"budget_tokens": 500}}},
		}},
		{"extra-body", modelConfig{
			CatwalkCfg: catwalk.Model{ID: "reasoner", CanReason: true, ReasoningLevels: levels, Options: catwalk.ModelOptions{ProviderOptions: map[string]any{"user": "catwalk-user", "extra_body": map[string]any{"catwalk": true, "custom": "catwalk"}}}},
			ModelCfg:   config.SelectedModel{ReasoningEffort: "low", ProviderOptions: map[string]any{"extra_body": map[string]any{"custom": "selected", "thinking": map[string]any{"type": "explicit"}}}},
		}},
		{"top-k", modelConfig{CatwalkCfg: catwalk.Model{ID: "plain-model", Options: catwalk.ModelOptions{TopK: &catwalkTopK}}, ModelCfg: config.SelectedModel{TopK: &topK}}},
		{"catwalk-top-k", modelConfig{CatwalkCfg: catwalk.Model{ID: "plain-model", Options: catwalk.ModelOptions{TopK: &catwalkTopK}}}},
		{"malformed", modelConfig{CatwalkCfg: catwalk.Model{ID: "plain-model"}, ModelCfg: config.SelectedModel{TopK: &topK, ProviderOptions: map[string]any{"user": 5.0}}}},
		{"malformed-no-top-k", modelConfig{CatwalkCfg: catwalk.Model{ID: "plain-model"}, ModelCfg: config.SelectedModel{ProviderOptions: map[string]any{"user": 5.0}}}},
		{"gpt-5-fast", modelConfig{CatwalkCfg: catwalk.Model{ID: "gpt-5", CanReason: true, ReasoningLevels: []string{"minimal", "low", "medium", "high"}}, ModelCfg: config.SelectedModel{ReasoningEffort: "high", ServiceTier: "fast"}}},
		{"gpt-5.2-standard", modelConfig{CatwalkCfg: catwalk.Model{ID: "gpt-5.2", CanReason: true, ReasoningLevels: levels}, ModelCfg: config.SelectedModel{ServiceTier: "standard"}}},
		{"gpt-5-pro", modelConfig{CatwalkCfg: catwalk.Model{ID: "gpt-5-pro", CanReason: true, ReasoningLevels: levels}, ModelCfg: config.SelectedModel{ServiceTier: "fast"}}},
		{"gpt-4o", modelConfig{CatwalkCfg: catwalk.Model{ID: "gpt-4o"}}},
		{"abacus-format", modelConfig{CatwalkCfg: catwalk.Model{ID: "custom-x", CanReason: true, ReasoningLevels: levels, Options: catwalk.ModelOptions{ProviderOptions: map[string]any{config.AbacusAPIFormatOption: "responses"}}}}},
		{"gemini-2", modelConfig{CatwalkCfg: catwalk.Model{ID: "gemini-2.5-pro", CanReason: true, ReasoningLevels: levels}}},
		{"gemini-3", modelConfig{CatwalkCfg: catwalk.Model{ID: "gemini-3-pro", CanReason: true, ReasoningLevels: []string{"low", "high"}}, ModelCfg: config.SelectedModel{ReasoningEffort: "high"}}},
		{"minimax-m3", modelConfig{CatwalkCfg: catwalk.Model{ID: "MiniMax-M3", CanReason: true, ReasoningLevels: levels}, ModelCfg: config.SelectedModel{ReasoningEffort: "high"}}},
		{"minimax-m2-think", modelConfig{CatwalkCfg: catwalk.Model{ID: "minimax-m2", CanReason: true}, ModelCfg: config.SelectedModel{Think: true}}},
		{"minimax-off", modelConfig{CatwalkCfg: catwalk.Model{ID: "minimax-m2", CanReason: true}}},
		{"deepseek-none", modelConfig{CatwalkCfg: catwalk.Model{ID: "deepseek-v4", CanReason: true, ReasoningLevels: []string{"none", "high"}}, ModelCfg: config.SelectedModel{ReasoningEffort: "none"}}},
		{"zai-org", modelConfig{CatwalkCfg: catwalk.Model{ID: "zai-org/glm-5", CanReason: true}, ModelCfg: config.SelectedModel{Think: true}}},
		{"claude", modelConfig{CatwalkCfg: catwalk.Model{ID: "claude-opus-5", CanReason: true, ReasoningLevels: []string{"low", "high", "max"}}, ModelCfg: config.SelectedModel{ReasoningEffort: "max"}}},
		{"claude-think", modelConfig{CatwalkCfg: catwalk.Model{ID: "claude-sonnet-5", CanReason: true}, ModelCfg: config.SelectedModel{Think: true}}},
	}
}

// TestProviderOptionsCharacterization pins Options' output for
// every provider type and quirky provider ID against a range of model
// selections, so restructuring it cannot change what reaches a provider.
func TestProviderOptionsCharacterization(t *testing.T) {
	var b strings.Builder
	for _, provider := range providerOptionsProviders() {
		for _, variant := range providerOptionsModels() {
			model := variant.model
			model.ModelCfg.Provider = provider.ID
			model.ModelCfg.Model = model.CatwalkCfg.ID
			fmt.Fprintf(&b, "== %s/%s %s\n", provider.Type, provider.ID, variant.name)
			if len(provider.ExtraBody) > 0 {
				fmt.Fprintf(&b, "   (provider extra body)\n")
			}
			if len(provider.ProviderOptions) > 0 {
				fmt.Fprintf(&b, "   (provider options)\n")
			}
			opts := Options(model.CatwalkCfg, model.ModelCfg, provider)
			keys := make([]string, 0, len(opts))
			for key := range opts {
				keys = append(keys, key)
			}
			slices.Sort(keys)
			for _, key := range keys {
				fmt.Fprintf(&b, "%s: %s\n", key, dumpValue(reflect.ValueOf(opts[key])))
			}
		}
	}

	golden := filepath.Join("testdata", "provider_options.golden")
	if *updateProviderOptions {
		require.NoError(t, os.WriteFile(golden, []byte(b.String()), 0o644))
	}
	want, err := os.ReadFile(golden)
	require.NoError(t, err)
	require.Equal(t, string(want), b.String())
}

// dumpValue renders v with the Go types of everything held in interfaces,
// and map keys sorted, so the dump is both exact and deterministic.
func dumpValue(v reflect.Value) string {
	switch v.Kind() {
	case reflect.Invalid:
		return "nil"
	case reflect.Interface:
		if v.IsNil() {
			return "nil"
		}
		return fmt.Sprintf("(%s)%s", v.Elem().Type(), dumpValue(v.Elem()))
	case reflect.Pointer:
		if v.IsNil() {
			return "nil"
		}
		return "&" + dumpValue(v.Elem())
	case reflect.Map:
		if v.IsNil() {
			return "nil"
		}
		keys := v.MapKeys()
		slices.SortFunc(keys, func(a, b reflect.Value) int {
			return strings.Compare(fmt.Sprint(a.Interface()), fmt.Sprint(b.Interface()))
		})
		parts := make([]string, 0, len(keys))
		for _, key := range keys {
			parts = append(parts, fmt.Sprintf("%v: %s", key.Interface(), dumpValue(v.MapIndex(key))))
		}
		return "{" + strings.Join(parts, ", ") + "}"
	case reflect.Slice, reflect.Array:
		if v.Kind() == reflect.Slice && v.IsNil() {
			return "nil"
		}
		parts := make([]string, 0, v.Len())
		for i := range v.Len() {
			parts = append(parts, dumpValue(v.Index(i)))
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case reflect.Struct:
		parts := make([]string, 0, v.NumField())
		for i := range v.NumField() {
			field := v.Field(i)
			if field.IsZero() {
				continue
			}
			parts = append(parts, v.Type().Field(i).Name+": "+dumpValue(field))
		}
		return v.Type().Name() + "{" + strings.Join(parts, ", ") + "}"
	case reflect.String:
		return fmt.Sprintf("%q", v.String())
	case reflect.Bool:
		return fmt.Sprint(v.Bool())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return fmt.Sprint(v.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return fmt.Sprint(v.Uint())
	case reflect.Float32, reflect.Float64:
		return fmt.Sprint(v.Float())
	default:
		if v.CanInterface() {
			return fmt.Sprint(v.Interface())
		}
		return v.String()
	}
}

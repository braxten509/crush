package config

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/charmbracelet/crush/internal/csync"
	"github.com/stretchr/testify/require"
)

const grokMetadataFixture = `{"jsonrpc":"2.0","id":1,"result":{"_meta":{"modelState":{"availableModels":[
 {"modelId":"grok-4.7","name":"Grok 4.7","_meta":{"totalContextTokens":256000,"supportsReasoningEffort":true,"reasoningEffort":"xhigh","reasoningEfforts":[{"value":"xhigh"},{"value":"high","default":true},{"value":"medium"},{"value":"low"}]}},
 {"modelId":"grok-4.5","name":"Grok 4.5","_meta":{"supportsReasoningEffort":true,"reasoningEfforts":[{"value":"high","default":true},{"value":"medium"},{"value":"low"}]}},
 {"modelId":"future-model","_meta":{"supportsReasoningEffort":true,"reasoningEffort":"careful","reasoningEfforts":[{"id":"quick"},{"id":"careful"},{"value":"quick"},{}]}},
 {"modelId":"plain-model","_meta":{"supportsReasoningEffort":false,"reasoningEfforts":[{"value":"high","default":true}]}},
 {"name":"missing ID"}
]}}}}`

func TestGrokModelCapabilities(t *testing.T) {
	models, err := grokModelMetadata([]byte(grokMetadataFixture))
	require.NoError(t, err)
	require.Len(t, models, 4)
	require.Equal(t, "Grok 4.7", models[0].Name)
	require.Equal(t, int64(256000), models[0].ContextWindow)
	require.True(t, models[0].CanReason)
	require.Equal(t, []string{"xhigh", "high", "medium", "low"}, models[0].ReasoningLevels)
	require.Equal(t, "high", models[0].DefaultReasoningEffort, "use the declared default, not the current user's override")
	require.Equal(t, []string{"high", "medium", "low"}, models[1].ReasoningLevels)
	require.Equal(t, []string{"quick", "careful"}, models[2].ReasoningLevels)
	require.Equal(t, "careful", models[2].DefaultReasoningEffort)
	require.False(t, models[3].CanReason)
	require.NotNil(t, models[3].ReasoningLevels, "explicitly unsupported controls must override any stale fallback")
	require.Empty(t, models[3].ReasoningLevels)
	for _, data := range []string{`not json`, `{"id":1,"result":{}}`, `{"id":1,"error":{"message":"unavailable"}}`} {
		_, err := grokModelMetadata([]byte(data))
		require.Error(t, err)
	}
}

func TestGrokDiscoveryAndCacheKeepEffortControls(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	arguments := filepath.Join(dir, "arguments")
	t.Setenv("GROK_TEST_ARGUMENTS", arguments)
	write := func(output string) {
		var compact bytes.Buffer
		require.NoError(t, json.Compact(&compact, []byte(output)))
		script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$GROK_TEST_ARGUMENTS\"\nread -r request\ncat <<'EOF'\n" + compact.String() + "\nEOF\n"
		require.NoError(t, os.WriteFile(filepath.Join(dir, "grok"), []byte(script), 0o755))
	}
	write(grokMetadataFixture)
	models, err := discoverGrok(t.Context())
	require.NoError(t, err)
	args, err := os.ReadFile(arguments)
	require.NoError(t, err)
	require.Equal(t, "agent\n--no-leader\nstdio\n", string(args))

	refreshCLIModels(dir)
	cached := cachedModels(string(TypeGrokCLI))
	require.Len(t, cached, len(models))
	for i := range cached {
		require.True(t, slices.Equal(models[i].ReasoningLevels, cached[i].ReasoningLevels))
		require.Equal(t, models[i].DefaultReasoningEffort, cached[i].DefaultReasoningEffort)
		require.Equal(t, models[i].ContextWindow, cached[i].ContextWindow)
	}
	cfg := &Config{Providers: csync.NewMapFrom(map[string]ProviderConfig{
		string(TypeGrokCLI): {ID: string(TypeGrokCLI), Models: cached},
	})}
	require.NoError(t, cfg.ValidateReasoningEffort(string(TypeGrokCLI), "grok-4.7", "xhigh"))
	require.Error(t, cfg.ValidateReasoningEffort(string(TypeGrokCLI), "grok-4.5", "xhigh"))

	// A failed metadata refresh must keep the last usable choices.
	write(`{"id":1,"error":{"message":"offline"}}`)
	refreshCLIModels(dir)
	require.Equal(t, cached, cachedModels(string(TypeGrokCLI)))
}

package cmd

import (
	"bytes"
	"encoding/json"
	"testing"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/csync"
	"github.com/stretchr/testify/require"
)

func TestPrintModelsJSON(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{Providers: csync.NewMapFrom(map[string]config.ProviderConfig{
		"codex-cli": {ID: "codex-cli", Name: "Codex", Type: config.TypeCodexCLI, Models: []catwalk.Model{
			{ID: "gpt-6.1-sol", Name: "GPT-6.1 Sol", ReasoningLevels: []string{"low", "high"}, DefaultReasoningEffort: "high"},
		}},
		"grok-cli": {ID: "grok-cli", Name: "Grok", Type: config.TypeGrokCLI, Models: []catwalk.Model{{ID: "grok-4.7", Name: "Grok 4.7"}}},
		"off":      {ID: "off", Disable: true, Models: []catwalk.Model{{ID: "hidden"}}},
	})}

	var out bytes.Buffer
	require.NoError(t, printModelsJSON(&out, cfg, ""))
	var list []modelInfo
	require.NoError(t, json.Unmarshal(out.Bytes(), &list))
	require.Equal(t, []modelInfo{
		{Provider: "codex-cli", ProviderName: "Codex", Model: "gpt-6.1-sol", Name: "GPT-6.1 Sol", ReasoningLevels: []string{"low", "high"}, DefaultReasoningEffort: "high", Fast: true},
		{Provider: "grok-cli", ProviderName: "Grok", Model: "grok-4.7", Name: "Grok 4.7", ReasoningLevels: []string{}},
	}, list)

	out.Reset()
	require.NoError(t, printModelsJSON(&out, cfg, "grok"))
	require.NoError(t, json.Unmarshal(out.Bytes(), &list))
	require.Len(t, list, 1)
}

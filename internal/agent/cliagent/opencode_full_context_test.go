package cliagent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/stretchr/testify/require"
)

// Keeping the free DeepSeek's whole context also turns OpenCode's own
// compaction off, and only while that switch is on (owner, 2026-10-23).
func TestOpenCodeEnvTurnsItsCompactionOff(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	cache, err := os.UserCacheDir()
	require.NoError(t, err)
	file := filepath.Join(cache, "crush", "opencode", "opencode.json")

	_, signal := opencodeEnv(true)
	require.NotEmpty(t, signal)
	data, err := os.ReadFile(file)
	require.NoError(t, err)
	var config struct {
		Compaction struct {
			Auto *bool `json:"auto"`
		} `json:"compaction"`
	}
	require.NoError(t, json.Unmarshal(data, &config))
	require.NotNil(t, config.Compaction.Auto)
	require.False(t, *config.Compaction.Auto, "OpenCode no longer compacts the chat by itself")

	_, _ = opencodeEnv(false)
	_, err = os.Stat(file)
	require.True(t, os.IsNotExist(err), "every other model gets OpenCode's own compaction back")
}

// The meter measures against the window Crush gives the model, not the one
// OpenCode reports, when the switch keeps its whole context.
func TestOpenCodeBudgetUsesTheWindowCrushGives(t *testing.T) {
	const reported = int64(200_000)
	require.Equal(t, reported, openCodeBudgetWindow(reported, &Model{}))
	require.EqualValues(t, 1_000_000, openCodeBudgetWindow(reported, &Model{ContextWindow: 1_000_000}))
	require.EqualValues(t, 968_000, openCodeContextBudget(openCodeBudgetWindow(reported, &Model{ContextWindow: 1_000_000})).ContextLimit)
}

// Only the free DeepSeek gets Crush's window; every other OpenCode Zen model
// keeps OpenCode's own compaction.
func TestProviderGivesOnlyTheFreeDeepSeekItsWholeContext(t *testing.T) {
	for _, on := range []bool{true, false} {
		provider := NewProvider(config.TypeOpenCodeZenCLI, t.TempDir(), t.TempDir(), nil, nil, "", false, "", on)
		free, err := provider.LanguageModel(t.Context(), config.OpenCodeZenFreeDeepSeekModel)
		require.NoError(t, err)
		if on {
			require.EqualValues(t, config.OpenCodeZenFreeDeepSeekWindow, free.(*Model).ContextWindow)
		} else {
			require.Zero(t, free.(*Model).ContextWindow)
		}
		other, err := provider.LanguageModel(t.Context(), "opencode/beam-free")
		require.NoError(t, err)
		require.Zero(t, other.(*Model).ContextWindow)
	}
}

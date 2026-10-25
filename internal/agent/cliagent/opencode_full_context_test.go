package cliagent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/stretchr/testify/require"
)

// Keeping the free DeepSeek's whole context also turns OpenCode's own
// compaction off, and only while that switch is on (owner, 2026-10-23).
// Two chats with different switches can start OpenCode at the same time,
// so neither may change the other's config.
func TestOpenCodeEnvTurnsItsCompactionOff(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	configDir := func(env []string) string {
		for _, e := range env {
			if dir, ok := strings.CutPrefix(e, "OPENCODE_CONFIG_DIR="); ok {
				return dir
			}
		}
		t.Fatal("no OPENCODE_CONFIG_DIR")
		return ""
	}

	whole, signal := opencodeEnv(true)
	require.NotEmpty(t, signal)
	other, _ := opencodeEnv(false)

	data, err := os.ReadFile(filepath.Join(configDir(whole), "opencode.json"))
	require.NoError(t, err, "a later chat leaves the whole-context chat's config")
	var config struct {
		Compaction struct {
			Auto *bool `json:"auto"`
		} `json:"compaction"`
	}
	require.NoError(t, json.Unmarshal(data, &config))
	require.NotNil(t, config.Compaction.Auto)
	require.False(t, *config.Compaction.Auto, "OpenCode no longer compacts the chat by itself")

	_, err = os.Stat(filepath.Join(configDir(other), "opencode.json"))
	require.True(t, os.IsNotExist(err), "every other model gets OpenCode's own compaction back")
	for _, env := range [][]string{whole, other} {
		_, err = os.Stat(filepath.Join(configDir(env), "tools", "bash.ts"))
		require.NoError(t, err, "both keep Crush's bash tool")
	}
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

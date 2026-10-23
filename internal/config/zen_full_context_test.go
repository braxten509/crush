package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/csync"
	"github.com/stretchr/testify/require"
)

// The switch that keeps OpenCode Zen's free DeepSeek on its whole window
// changes that model only; every other model keeps the size its CLI reports
// (owner, 2026-10-23).
func TestZenFreeDeepSeekKeepsWholeContext(t *testing.T) {
	isolateModelCache(t)
	file := cliModelCachePath()
	require.NoError(t, os.MkdirAll(filepath.Dir(file), 0o700))
	cache := map[string][]catwalk.Model{
		string(TypeOpenCodeZenCLI): {
			{ID: "opencode/deepseek-v4.1-flash-free", Name: "DeepSeek V4.1 Flash Free", ContextWindow: 200_000, DefaultMaxTokens: 32_000},
			{ID: "opencode/flint-40b-free", Name: "Flint 40B Free", ContextWindow: 262_144, DefaultMaxTokens: 65_536},
		},
	}
	data, err := json.Marshal(cache)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(file, data, 0o600))

	windows := func(options *Options) map[string]int64 {
		cfg := &Config{Options: options, Providers: csync.NewMapFrom(map[string]ProviderConfig{
			string(TypeOpenCodeZenCLI): {Name: "OpenCode Zen"},
		})}
		cfg.addCLIProviders("")
		zen, ok := cfg.Providers.Get(string(TypeOpenCodeZenCLI))
		require.True(t, ok)
		out := map[string]int64{}
		for _, model := range zen.Models {
			out[model.ID] = model.ContextWindow
		}
		return out
	}

	off := windows(&Options{})
	require.EqualValues(t, 200_000, off["opencode/deepseek-v4.1-flash-free"])

	on := windows(&Options{OpenCodeZenFreeDeepSeekFullContext: true})
	require.EqualValues(t, 1_000_000, on["opencode/deepseek-v4.1-flash-free"])
	require.EqualValues(t, 262_144, on["opencode/flint-40b-free"], "other models keep the window their CLI reports")

	// Turning it back off restores what the CLI reports.
	require.EqualValues(t, 200_000, windows(&Options{})["opencode/deepseek-v4.1-flash-free"])
}

// A provider registered for the first time (no saved settings) gets the same
// window: the switch decides, not the size Zen reports.
func TestZenFreeDeepSeekKeepsWholeContextWhenRegistering(t *testing.T) {
	detectCLIs = true
	t.Cleanup(func() { detectCLIs = false })
	isolateModelCache(t)

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "opencode"), []byte(`#!/bin/sh
if [ "$2" = "opencode" ]; then
printf 'opencode/deepseek-v4.1-flash-free\n{"id":"deepseek-v4.1-flash-free","name":"DeepSeek V4.1 Flash Free","cost":{"input":0,"output":0},"limit":{"context":200000,"output":32000}}\n'
fi
`), 0o755))

	cfg := &Config{Options: &Options{OpenCodeZenFreeDeepSeekFullContext: true}, Providers: csync.NewMapFrom(map[string]ProviderConfig{})}
	cfg.addCLIProviders(dir)
	zen, ok := cfg.Providers.Get(string(TypeOpenCodeZenCLI))
	require.True(t, ok)
	require.NotEmpty(t, zen.Models)
	var window int64
	for _, model := range zen.Models {
		if model.ID == OpenCodeZenFreeDeepSeekModel {
			window = model.ContextWindow
		}
	}
	require.EqualValues(t, 1_000_000, window)
}

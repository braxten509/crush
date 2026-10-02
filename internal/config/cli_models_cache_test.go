package config

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/csync"
	"github.com/stretchr/testify/require"
)

func TestConfiguredCLIProviderUsesDiscoveredModels(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	file := cliModelCachePath()
	require.NoError(t, os.MkdirAll(filepath.Dir(file), 0o700))
	cache := map[string][]catwalk.Model{
		string(TypeGrokCLI):  {{ID: "new-grok-model", Name: "New model", CanReason: true, ReasoningLevels: []string{"careful"}, DefaultReasoningEffort: "careful"}},
		string(TypeCodexCLI): {{ID: "text-only", Name: "Text only", SupportsImages: false}, {ID: "vision", Name: "Vision", SupportsImages: true}},
	}
	data, err := json.Marshal(cache)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(file, data, 0o600))
	cfg := &Config{Providers: csync.NewMapFrom(map[string]ProviderConfig{
		string(TypeGrokCLI):  {Name: "My Grok", Disable: true},
		string(TypeCodexCLI): {Name: "My Codex"},
	})}
	cfg.addCLIProviders("")
	grok, ok := cfg.Providers.Get(string(TypeGrokCLI))
	require.True(t, ok)
	require.Equal(t, "My Grok", grok.Name)
	require.True(t, grok.Disable)
	require.Len(t, grok.Models, 1)
	require.Equal(t, "new-grok-model", grok.Models[0].ID)
	require.Equal(t, []string{"careful"}, grok.Models[0].ReasoningLevels)
	require.Equal(t, "careful", grok.Models[0].DefaultReasoningEffort)
	require.True(t, grok.Models[0].SupportsImages)
	require.NotNil(t, cfg.GetModel(string(TypeGrokCLI), "new-grok-model"))
	codex, ok := cfg.Providers.Get(string(TypeCodexCLI))
	require.True(t, ok)
	require.Equal(t, cache[string(TypeCodexCLI)], codex.Models)

	explicit := []catwalk.Model{{ID: "user-model", Name: "User model"}}
	cfg.Providers.Set(string(TypeGrokCLI), ProviderConfig{Models: explicit})
	cfg.addCLIProviders("")
	grok, _ = cfg.Providers.Get(string(TypeGrokCLI))
	require.Equal(t, explicit, grok.Models, "explicit model lists remain overrides")
}

func TestConcurrentCLIModelRefreshesPublishCompleteJSON(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "codex"), nil, 0o700))
	file := cliModelCachePath()
	require.NoError(t, os.MkdirAll(filepath.Dir(file), 0o700))
	require.NoError(t, os.WriteFile(file, []byte(`{}`), 0o600))
	// A temporary file owned by another refresh must never be touched.
	require.NoError(t, os.WriteFile(file+".tmp", []byte("another writer"), 0o600))
	saved := cliDiscovery
	t.Cleanup(func() { cliDiscovery = saved })
	var sequence atomic.Int32
	cliDiscovery = map[catwalk.Type]func(context.Context, string) ([]catwalk.Model, error){
		TypeCodexCLI: func(context.Context, string) ([]catwalk.Model, error) {
			count := sequence.Add(1)
			return []catwalk.Model{{ID: fmt.Sprint(count), Name: strings.Repeat("model", int(count)*1000)}}, nil
		},
	}
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() { refreshCLIModels(dir) })
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	var readErr error
	for {
		data, err := os.ReadFile(file)
		if err != nil {
			readErr = err
		} else if !json.Valid(data) {
			readErr = fmt.Errorf("refresh published incomplete JSON")
		}
		select {
		case <-done:
			require.NoError(t, readErr)
			require.Len(t, cachedModels(string(TypeCodexCLI)), 1)
			data, err := os.ReadFile(file + ".tmp")
			require.NoError(t, err)
			require.Equal(t, "another writer", string(data))
			entries, err := os.ReadDir(filepath.Dir(file))
			require.NoError(t, err)
			require.Len(t, entries, 2, "unique temporary files are cleaned up")
			return
		default:
		}
	}
}

func TestCLIModelRefreshUsesSuppliedPATH(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	dir := t.TempDir()
	script := `#!/bin/sh
read -r line
read -r line
read -r line
echo '{"id":2,"result":{"data":[{"id":"selected-path-model"}]}}'
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "codex"), []byte(script), 0o700))
	// The process PATH cannot locate this CLI; detection and execution must
	// agree on the explicit PATH passed to refreshCLIModels.
	t.Setenv("PATH", t.TempDir())
	refreshCLIModels(dir)
	models := cachedModels(string(TypeCodexCLI))
	require.Len(t, models, 1)
	require.Equal(t, "selected-path-model", models[0].ID)
}

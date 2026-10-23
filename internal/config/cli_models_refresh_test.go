package config

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/charmbracelet/crush/internal/csync"
	"github.com/stretchr/testify/require"
)

// The first launch after a CLI reports a new model or effort level showed
// the list from before it: the refresh ran after the providers were built,
// so a level appeared one launch late (owner, 2026-10-22).
func TestStaleModelListIsReadBeforeTheLoadReturns(t *testing.T) {
	isolateModelCache(t)
	isolateModelRefresh(t)
	writeModelCache(t, 2*time.Hour)
	var refreshed atomic.Bool
	refreshCLIModelList = func(string) {
		time.Sleep(20 * time.Millisecond)
		refreshed.Store(true)
	}
	maybeRefreshCLIModels("")
	require.True(t, refreshed.Load(), "a stale list is read before the load goes on")
}

func TestFreshModelListIsNotReadAgain(t *testing.T) {
	isolateModelCache(t)
	isolateModelRefresh(t)
	writeModelCache(t, 0)
	var calls atomic.Int32
	refreshCLIModelList = func(string) { calls.Add(1) }
	maybeRefreshCLIModels("")
	require.Zero(t, calls.Load(), "a list younger than the TTL is already current")
}

// A CLI that hangs must not hold the app's start: the load goes on with the
// list it has and the refresh finishes on its own.
func TestSlowModelRefreshDoesNotHoldTheLoad(t *testing.T) {
	isolateModelCache(t)
	isolateModelRefresh(t)
	writeModelCache(t, 2*time.Hour)
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })
	refreshCLIModelList = func(string) { <-block }
	wait := modelRefreshWait
	modelRefreshWait = 20 * time.Millisecond
	t.Cleanup(func() { modelRefreshWait = wait })

	start := time.Now()
	maybeRefreshCLIModels("")
	require.Less(t, time.Since(start), 2*time.Second, "the refresh wait is capped")
}

// An engine reloads when its config files change, so a list refreshed in
// the background must read as one: that is how an open app gets the new
// levels without a restart.
func TestRefreshedModelListReadsAsAConfigChange(t *testing.T) {
	isolateModelCache(t)
	writeModelCache(t, 2*time.Hour)
	store, err := Load(t.TempDir(), t.TempDir(), false)
	require.NoError(t, err)
	require.False(t, store.ConfigStaleness().Dirty)

	require.NoError(t, os.WriteFile(cliModelCachePath(), []byte(`{"refreshed":true}`), 0o600))
	require.True(t, store.ChangedOnDisk(), "a refreshed model list is a config change")
}

// The providers are read from the cache a refresh has just written, not the
// stale list the launch found (owner, 2026-10-22: this is why a level their
// CLI already reported showed up one launch late).
func TestProvidersAreReadFromTheRefreshedList(t *testing.T) {
	isolateModelCache(t)
	isolateModelRefresh(t)
	require.NoError(t, os.MkdirAll(filepath.Dir(cliModelCachePath()), 0o700))
	require.NoError(t, os.WriteFile(cliModelCachePath(), []byte(`{"codex-cli":[{"id":"stale-model","name":"Stale"}]}`), 0o600))
	stale := time.Now().Add(-2 * time.Hour)
	require.NoError(t, os.Chtimes(cliModelCachePath(), stale, stale))

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "codex"), nil, 0o700))
	refreshCLIModelList = func(string) {
		fresh := `{"codex-cli":[{"id":"fresh-model","name":"Fresh","reasoning_levels":["low","high"]}]}`
		require.NoError(t, os.WriteFile(cliModelCachePath(), []byte(fresh), 0o600))
	}
	detect, refresh := detectCLIs, refreshModels
	detectCLIs, refreshModels = true, true
	t.Cleanup(func() { detectCLIs, refreshModels = detect, refresh })

	cfg := &Config{Providers: csync.NewMapFrom(map[string]ProviderConfig{})}
	cfg.addCLIProviders(dir)

	codex, ok := cfg.Providers.Get(string(TypeCodexCLI))
	require.True(t, ok)
	require.Len(t, codex.Models, 1)
	require.Equal(t, "fresh-model", codex.Models[0].ID)
	require.Equal(t, []string{"low", "high"}, codex.Models[0].ReasoningLevels)
}

// writeModelCache writes an empty model cache aged by age.
func writeModelCache(t *testing.T, age time.Duration) {
	t.Helper()
	file := cliModelCachePath()
	require.NoError(t, os.MkdirAll(filepath.Dir(file), 0o700))
	require.NoError(t, os.WriteFile(file, []byte(`{}`), 0o600))
	at := time.Now().Add(-age)
	require.NoError(t, os.Chtimes(file, at, at))
}

// isolateModelRefresh leaves the process-wide refresh as it was.
func isolateModelRefresh(t *testing.T) {
	t.Helper()
	refreshOnce = sync.Once{}
	refresh := refreshCLIModelList
	t.Cleanup(func() {
		refreshOnce = sync.Once{}
		refreshCLIModelList = refresh
	})
}

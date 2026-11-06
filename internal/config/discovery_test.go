package config

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"charm.land/catwalk/v3/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/csync"
	"github.com/charmbracelet/crush/internal/env"
	"github.com/stretchr/testify/require"
)

// modelsServer serves an OpenAI-style /models listing whose IDs can be
// swapped mid-test, and counts the requests it receives.
type modelsServer struct {
	*httptest.Server
	mu       sync.Mutex
	ids      []string
	requests atomic.Int32
}

func newModelsServer(t *testing.T, ids ...string) *modelsServer {
	t.Helper()
	s := &modelsServer{ids: ids}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		s.requests.Add(1)
		s.mu.Lock()
		defer s.mu.Unlock()
		entries := make([]string, len(s.ids))
		for i, id := range s.ids {
			entries[i] = fmt.Sprintf(`{"id": %q, "object": "model"}`, id)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"data": [%s]}`, strings.Join(entries, ","))
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *modelsServer) setIDs(ids ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ids = ids
}

func modelIDs(models []catwalk.Model) []string {
	ids := make([]string, len(models))
	for i, m := range models {
		ids[i] = m.ID
	}
	return ids
}

// configureCustom runs configureProviders for a single custom provider
// against a store whose discovery cache lives in dir.
func configureCustom(t *testing.T, dir string, pc ProviderConfig) (*ConfigStore, *Config) {
	t.Helper()
	cfg := &Config{Providers: csync.NewMapFrom(map[string]ProviderConfig{"local": pc})}
	cfg.setDefaults(t.TempDir(), "")
	store := testStoreWithPath(cfg, dir)
	testEnv := env.NewFromMap(map[string]string{})
	_, jobs, err := cfg.configureProvidersCached(t.Context(), testEnv, NewShellVariableResolver(testEnv), nil, store.discoveryCacheDir())
	require.NoError(t, err)
	store.cachedDiscovery = jobs
	return store, cfg
}

func TestDiscovery_CacheServesLaterStartups(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	server := newModelsServer(t, "a", "b")
	pc := ProviderConfig{BaseURL: server.URL + "/v1"}

	store, cfg := configureCustom(t, dir, pc)
	p, ok := cfg.Providers.Get("local")
	require.True(t, ok)
	require.Equal(t, []string{"a", "b"}, modelIDs(p.Models))
	require.Empty(t, store.cachedDiscovery, "a fresh probe is not a cache hit")

	server.Close()

	store, cfg = configureCustom(t, dir, pc)
	p, ok = cfg.Providers.Get("local")
	require.True(t, ok, "cached models keep an unreachable provider usable")
	require.Equal(t, []string{"a", "b"}, modelIDs(p.Models))
	require.Len(t, store.cachedDiscovery, 1)
}

func TestDiscovery_BaseURLChangeInvalidatesCache(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	first := newModelsServer(t, "old")
	second := newModelsServer(t, "new")

	configureCustom(t, dir, ProviderConfig{BaseURL: first.URL + "/v1"})
	_, cfg := configureCustom(t, dir, ProviderConfig{BaseURL: second.URL + "/v1"})

	p, ok := cfg.Providers.Get("local")
	require.True(t, ok)
	require.Equal(t, []string{"new"}, modelIDs(p.Models))
}

func TestDiscovery_ConfiguredModelsWinOverCache(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	server := newModelsServer(t, "a", "b")
	discoverTrue := true
	pc := ProviderConfig{
		BaseURL:            server.URL + "/v1",
		AutoDiscoverModels: &discoverTrue,
		Models:             []catwalk.Model{{ID: "b", Name: "Mine", ContextWindow: 42}},
	}

	configureCustom(t, dir, pc)
	_, cfg := configureCustom(t, dir, pc)

	p, ok := cfg.Providers.Get("local")
	require.True(t, ok)
	require.Equal(t, []string{"b", "a"}, modelIDs(p.Models))
	require.Equal(t, "Mine", p.Models[0].Name)
	require.EqualValues(t, 42, p.Models[0].ContextWindow)
}

// loadIsolated loads a store whose only provider is the given custom one,
// so neither the host's config nor its environment can leak in.
func loadIsolated(t *testing.T, workDir string) *ConfigStore {
	t.Helper()
	resetProviderState()
	t.Cleanup(resetProviderState)
	store, err := Load(t.Context(), workDir, filepath.Join(workDir, ".crush"), false)
	require.NoError(t, err)
	return store
}

func writeGlobalConfig(t *testing.T, globalDir, body string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(globalDir, "crush.json"), []byte(body), 0o600))
}

func TestDiscovery_StartupKeepsSelectionAndRefreshesInBackground(t *testing.T) {
	globalDir := t.TempDir()
	workDir := t.TempDir()
	t.Setenv("CRUSH_GLOBAL_CONFIG", globalDir)
	t.Setenv("CRUSH_GLOBAL_DATA", globalDir)

	server := newModelsServer(t, "a", "b")
	writeGlobalConfig(t, globalDir, fmt.Sprintf(`{
		"options": {"disable_default_providers": true},
		"providers": {"local": {"base_url": %q}},
		"models": {
			"large": {"provider": "local", "model": "b"},
			"small": {"provider": "local", "model": "b"}
		}
	}`, server.URL+"/v1"))

	loadIsolated(t, workDir)

	server.setIDs("a", "b", "c")
	before := server.requests.Load()
	store := loadIsolated(t, workDir)
	require.Equal(t, before, server.requests.Load(), "a cached provider is not probed during startup")

	cfg := store.Config()
	start, ok := cfg.StartModel()
	require.True(t, ok)
	require.Equal(t, "b", start.Model)
	p, ok := cfg.Providers.Get("local")
	require.True(t, ok)
	require.Equal(t, []string{"a", "b"}, modelIDs(p.Models))

	reloaded, err := store.RefreshDiscoveredModels(t.Context())
	require.NoError(t, err)
	require.True(t, reloaded)
	p, ok = store.Config().Providers.Get("local")
	require.True(t, ok)
	require.Equal(t, []string{"a", "b", "c"}, modelIDs(p.Models))

	reloaded, err = store.RefreshDiscoveredModels(t.Context())
	require.NoError(t, err)
	require.False(t, reloaded, "an unchanged listing does not reload")
}

func TestDiscovery_ReloadDiscoversNewProvider(t *testing.T) {
	globalDir := t.TempDir()
	workDir := t.TempDir()
	t.Setenv("CRUSH_GLOBAL_CONFIG", globalDir)
	t.Setenv("CRUSH_GLOBAL_DATA", globalDir)

	known := newModelsServer(t, "a")
	writeGlobalConfig(t, globalDir, fmt.Sprintf(`{
		"options": {"disable_default_providers": true},
		"providers": {"local": {"base_url": %q}}
	}`, known.URL+"/v1"))
	store := loadIsolated(t, workDir)

	added := newModelsServer(t, "x", "y")
	writeGlobalConfig(t, globalDir, fmt.Sprintf(`{
		"options": {"disable_default_providers": true},
		"providers": {
			"local": {"base_url": %q},
			"added": {"base_url": %q}
		}
	}`, known.URL+"/v1", added.URL+"/v1"))
	require.NoError(t, store.ReloadFromDisk(t.Context()))

	p, ok := store.Config().Providers.Get("added")
	require.True(t, ok, "a provider added mid-session is discovered on reload")
	require.Equal(t, []string{"x", "y"}, modelIDs(p.Models))
}

func TestDiscovery_CacheUsesResolvedInputs(t *testing.T) {
	dir := t.TempDir()
	server := newModelsServer(t, "old")
	values := map[string]string{"KEY": "secret-one", "HEADER": "header-one"}
	resolver := NewShellVariableResolver(env.NewFromMap(values))
	job := newDiscoveryJob("local", ProviderConfig{BaseURL: server.URL + "/v1", APIKey: "$KEY", ExtraHeaders: map[string]string{"X-Token": "$HEADER"}}, nil, false)
	result := probeDiscovery(t.Context(), []discoveryJob{job}, resolver, dir)[job.id]
	require.NoError(t, result.err)
	_, ok := cachedDiscovery(dir, job, resolver)
	require.True(t, ok)
	data, err := os.ReadFile(discoveryCachePath(dir, job))
	require.NoError(t, err)
	require.NotContains(t, string(data), "secret-one")
	require.NotContains(t, string(data), "header-one")
	for _, changed := range []struct {
		key, header string
		kind        catwalk.Type
	}{
		{"secret-two", "header-one", job.cfg.Type},
		{"secret-one", "header-two", job.cfg.Type},
		{"secret-one", "header-one", catwalk.Type("other")},
	} {
		other := job
		other.providerType = string(changed.kind)
		other.cfg.Type = changed.kind
		r := NewShellVariableResolver(env.NewFromMap(map[string]string{"KEY": changed.key, "HEADER": changed.header}))
		_, ok = cachedDiscovery(dir, other, r)
		require.False(t, ok)
	}
}

func TestDiscovery_EmptySuccessReplacesCacheAndFailureKeepsIt(t *testing.T) {
	dir := t.TempDir()
	server := newModelsServer(t, "old")
	resolver := NewShellVariableResolver(env.NewFromMap(map[string]string{}))
	job := newDiscoveryJob("local", ProviderConfig{BaseURL: server.URL + "/v1"}, nil, false)
	require.NoError(t, probeDiscovery(t.Context(), []discoveryJob{job}, resolver, dir)[job.id].err)
	before, ok := cachedDiscovery(dir, job, resolver)
	require.True(t, ok)
	server.setIDs()
	result := probeDiscovery(t.Context(), []discoveryJob{job}, resolver, dir)[job.id]
	require.NoError(t, result.err)
	require.Empty(t, result.models)
	require.False(t, sameModels(before, result.models))
	empty, ok := cachedDiscovery(dir, job, resolver)
	require.True(t, ok)
	require.Empty(t, empty)
	server.Close()
	require.Error(t, probeDiscovery(t.Context(), []discoveryJob{job}, resolver, dir)[job.id].err)
	empty, ok = cachedDiscovery(dir, job, resolver)
	require.True(t, ok)
	require.Empty(t, empty)
}

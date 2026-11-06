package config

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/url"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"charm.land/catwalk/v3/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/config/providers"
	"github.com/charmbracelet/crush/internal/discover"
)

// discoveryProbeTimeout bounds the inline probe of the providers that have
// no cached models yet.
const discoveryProbeTimeout = 3 * time.Second

// discoveryJob is one provider whose models come from its /models endpoint.
type discoveryJob struct {
	id           string
	cfg          discover.Config
	providerType string
	// dynamic marks a catalog provider whose own API is its model list
	// (see dynamicModelProvider). Its answer is never cached, so it lists
	// exactly what the API returns on every load.
	dynamic bool
}

// discoveredModels contains a fingerprint of resolved discovery inputs, never credentials.
type discoveredModels struct {
	Fingerprint string          `json:"fingerprint"`
	Models      []catwalk.Model `json:"models"`
}

func discoveryFingerprint(job discoveryJob, resolver VariableResolver) string {
	base, err := resolver.ResolveValue(job.cfg.BaseURL)
	if err != nil {
		return ""
	}
	key, err := resolver.ResolveValue(job.cfg.APIKey)
	if err != nil {
		return ""
	}
	headers := make(map[string]string, len(job.cfg.ExtraHeaders))
	for name, value := range job.cfg.ExtraHeaders {
		resolved, err := resolver.ResolveValue(value)
		if err != nil {
			return ""
		}
		headers[name] = resolved
	}
	data, err := json.Marshal(struct {
		Type    string
		Base    string
		Key     string
		Headers map[string]string
	}{job.providerType, base, key, headers})
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// newDiscoveryJob describes the discovery of one provider's models. The
// configured type is kept as written: a provider still typed "google"
// lists its models through the Gemini API, and enrichers are looked up by
// the custom type (ollama, litellm, ...).
func newDiscoveryJob(id string, pc ProviderConfig, catalog []catwalk.Provider, dynamic bool) discoveryJob {
	providerType := cmp.Or(pc.Type, catwalk.TypeCompletions)
	cfg := discover.Config{
		ID:             cmp.Or(pc.ID, id),
		Type:           providerType,
		BaseURL:        pc.BaseURL,
		APIKey:         pc.APIKey,
		ExtraHeaders:   pc.ExtraHeaders,
		KnownProviders: catalog,
	}
	if dynamic {
		// A dynamic provider's catalog models supply the details of the
		// ids its API returns. Custom providers merge their configured
		// models afterwards instead (mergeModels), so the cache holds only
		// what the endpoint listed.
		cfg.ExistingModels = pc.Models
	}
	return discoveryJob{id: id, cfg: cfg, providerType: string(providerType), dynamic: dynamic}
}

// probeDiscovery discovers models for jobs concurrently, bounded by ctx,
// and caches every successful result of a cacheable job in dir (empty
// disables the cache). Every job gets a result, failures included, so the
// caller can tell a failed probe from a provider it never asked.
func probeDiscovery(ctx context.Context, jobs []discoveryJob, resolver VariableResolver, dir string) map[string]discoveryResult {
	results := make(map[string]discoveryResult, len(jobs))
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, job := range jobs {
		wg.Go(func() {
			models, err := discover.DiscoverModels(ctx, job.cfg, resolver)
			if err == nil {
				if enricher := discover.GetEnricher(job.providerType); enricher != nil && len(models) > 0 {
					models, _ = enricher.EnrichModels(ctx, job.cfg, resolver, models)
				}
				storeDiscovery(dir, job, resolver, models)
			}
			mu.Lock()
			results[job.id] = discoveryResult{models: models, err: err}
			mu.Unlock()
		})
	}
	wg.Wait()
	return results
}

// discoveryCachePath is where job's models are cached in dir, or empty when
// the cache is off for it.
func discoveryCachePath(dir string, job discoveryJob) string {
	if dir == "" || job.dynamic {
		return ""
	}
	return filepath.Join(dir, url.PathEscape(job.id)+".json")
}

// cachedDiscovery returns job's cached models, if the cache holds any for
// its resolved discovery inputs.
func cachedDiscovery(dir string, job discoveryJob, resolver VariableResolver) ([]catwalk.Model, bool) {
	path := discoveryCachePath(dir, job)
	if path == "" {
		return nil, false
	}
	entry, _, err := providers.NewCache[discoveredModels](path).Get()
	fingerprint := discoveryFingerprint(job, resolver)
	if err != nil || fingerprint == "" || entry.Fingerprint != fingerprint {
		return nil, false
	}
	return entry.Models, true
}

// storeDiscovery caches job's discovered models in dir.
func storeDiscovery(dir string, job discoveryJob, resolver VariableResolver, models []catwalk.Model) {
	path := discoveryCachePath(dir, job)
	if path == "" {
		return
	}
	fingerprint := discoveryFingerprint(job, resolver)
	if fingerprint == "" {
		return
	}
	entry := discoveredModels{Fingerprint: fingerprint, Models: models}
	if err := providers.NewCache[discoveredModels](path).Store(entry); err != nil {
		slog.Warn("Failed to cache discovered models", "provider", job.id, "error", err)
	}
}

// discoveryCacheDir is where this store caches discovered models, next to
// the global data config; empty when the store has no data path, which
// disables the cache.
func (s *ConfigStore) discoveryCacheDir() string {
	if s.globalDataPath == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(s.globalDataPath), "discovered_models")
}

// RefreshDiscoveredModels re-probes every provider whose models came from
// the discovery cache, and reloads the config when any of them changed. It
// reports whether a reload happened. Meant to run in the background after
// startup.
func (s *ConfigStore) RefreshDiscoveredModels(ctx context.Context) (bool, error) {
	s.writeMu.RLock()
	jobs := s.cachedDiscovery
	resolver := s.resolver
	s.writeMu.RUnlock()

	if len(jobs) == 0 {
		return false, nil
	}

	dir := s.discoveryCacheDir()
	before := make(map[string][]catwalk.Model, len(jobs))
	for _, job := range jobs {
		before[job.id], _ = cachedDiscovery(dir, job, resolver)
	}
	changed := false
	for id, result := range probeDiscovery(ctx, jobs, resolver, dir) {
		if result.err != nil {
			slog.Warn("Model discovery failed", "provider", id, "error", result.err)
			continue
		}
		changed = changed || !sameModels(before[id], result.models)
	}
	if !changed {
		return false, nil
	}
	return true, s.ReloadFromDisk(ctx)
}

// sameModels compares two model lists as they are cached, so a list read
// back from disk matches the one it was written from.
func sameModels(a, b []catwalk.Model) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	ja, errA := json.Marshal(a)
	jb, errB := json.Marshal(b)
	return errA == nil && errB == nil && bytes.Equal(ja, jb)
}

// mergeModels appends the discovered models whose IDs the config does not
// already list; models from the config win.
func mergeModels(configured, discovered []catwalk.Model) []catwalk.Model {
	listed := make(map[string]bool, len(configured))
	for _, m := range configured {
		listed[m.ID] = true
	}
	merged := slices.Clone(configured)
	for _, m := range discovered {
		if !listed[m.ID] {
			merged = append(merged, m)
		}
	}
	return merged
}

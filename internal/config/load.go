package config

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"time"
)

// Load loads the configuration from the default paths and returns a
// ConfigStore that owns both the pure-data Config and all runtime state.
func Load(workingDir, dataDir string, debug bool) (*ConfigStore, error) {
	store := &ConfigStore{
		workingDir:      workingDir,
		globalDataPath:  GlobalConfigData(),
		debug:           debug,
		outsideWorktree: !isInsideWorktree(),
	}

	store.loading.Store(true)
	res, err := buildConfig(context.Background(), buildParams{
		workingDir:      workingDir,
		dataDir:         dataDir,
		debug:           debug,
		outsideWorktree: store.outsideWorktree,
		refreshHyper:    store.refreshHyperToken,
		// Publish the merged config before the catalog loads: the Hyper
		// token refresher and the catalog refetches go through the store.
		onMerged: func(cfg *Config, files configFiles) {
			store.setConfig(cfg)
			store.workspacePath = files.workspace
			store.loadedPaths = files.loaded
		},
		onCatalog: store.refreshShadowCatalogs,
		envOwner:  store,
	})
	store.loading.Store(false)

	// Hold writeMu for the rest of the load so persisting the pipeline's
	// writes below cannot trigger an auto-reload.
	store.writeMu.Lock()
	defer store.writeMu.Unlock()

	if res != nil {
		store.applyPendingWrites(res.pending)
	}
	if err != nil {
		return nil, err
	}
	store.publishBuild(res)

	// Persist any fallback corrections while we still hold writeMu. Only
	// the initial load does this: a reload must not write a fallback into
	// the config file it shares with sibling instances.
	if res.configured {
		if err := store.persistModelFallbacks(res.models); err != nil {
			return nil, err
		}
	}

	// Capture initial staleness snapshot. Track every discovered config path,
	// not just the ones that loaded, so a config file created after startup
	// (e.g. a crushrc added mid-session) is detected as a change.
	store.captureStalenessSnapshot(append(slices.Clone(res.files.discovered), res.files.loaded...))

	return store, nil
}

// refreshHyperToken refreshes the global Hyper OAuth token. It is the
// HyperTokenRefresher handed to the provider catalog fetch.
func (s *ConfigStore) refreshHyperToken(ctx context.Context) error {
	return s.RefreshOAuthToken(ctx, ScopeGlobal, "hyper")
}

// refreshShadowCatalogs renews the model catalogs that shadow Catwalk's
// when Catwalk refreshed its catalog this run: the ChatGPT and Grok
// catalogs are otherwise only fetched at login and would freeze there
// while Catwalk keeps moving. Best effort; a failed fetch keeps the
// catalog loaded from config.
//
// The refetches publish a copy-on-write config, so it returns the live
// config: the rest of the load must land on it rather than on a snapshot
// the store has already replaced. Only the initial load does this, so a
// reload never makes network calls for these catalogs.
func (s *ConfigStore) refreshShadowCatalogs(cfg *Config) *Config {
	if !CatwalkUpdated() {
		return cfg
	}
	fetchCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s.refetchOpenAIModels(fetchCtx, ScopeGlobal)
	s.refetchGrokModels(fetchCtx, ScopeGlobal)
	return s.Config()
}

// publishBuild installs the result of buildConfig on the store. Caller
// must hold writeMu.
func (s *ConfigStore) publishBuild(res *buildResult) {
	s.setConfig(res.cfg)
	s.loadedPaths = res.files.loaded
	s.workspacePath = res.files.workspace
	s.resolver = res.resolver
	s.knownProviders = res.knownProviders
}

// persistModelFallbacks writes the helper model to the global config when
// resolution corrected it or chose the default one. Caller must hold
// writeMu.
func (s *ConfigStore) persistModelFallbacks(resolved resolvedModels) error {
	if resolved.WriteSmall {
		if err := s.updateLocked(ScopeGlobal, func(c *Config) map[string]any {
			return s.helperModelFields(c, resolved.Small)
		}); err != nil {
			return fmt.Errorf("failed to update preferred small model: %w", err)
		}
	}
	return nil
}

// ReloadFromDisk re-runs the config load/merge flow and updates the in-memory
// config atomically. It rebuilds the staleness snapshot after successful reload.
// On failure, the store state is left as it was.
// Concurrent calls are serialised via writeMu.
func (s *ConfigStore) ReloadFromDisk(ctx context.Context) error {
	if s.workingDir == "" {
		return fmt.Errorf("cannot reload: working directory not set")
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.reloadFromDiskLocked(ctx)
}

// reloadFromDiskLocked performs the actual reload. Caller must hold writeMu.
//
// It runs the same pipeline as Load. The new Config is finished before it
// is published, since existing agents may read Config concurrently; on
// failure the store is left untouched.
func (s *ConfigStore) reloadFromDiskLocked(ctx context.Context) error {
	// Keep the data directory the store already resolved.
	var dataDir string
	if cur := s.Config(); cur != nil && cur.Options != nil {
		dataDir = cur.Options.DataDirectory
	}

	res, err := buildConfig(ctx, buildParams{
		workingDir:      s.workingDir,
		dataDir:         dataDir,
		debug:           s.debug,
		outsideWorktree: s.outsideWorktree,
		modelOverrides:  s.overrides.Models,
		refreshHyper:    s.refreshHyperToken,
		reload:          true,
		envOwner:        s,
	})
	if res != nil {
		s.applyPendingWrites(res.pending)
	}
	if err != nil {
		return err
	}
	s.publishBuild(res)

	// Rebuild staleness tracking. Track every discovered config path, not
	// just the ones that loaded, so a config file created after this reload
	// is detected as a change on the next staleness check.
	s.captureStalenessSnapshot(append(slices.Clone(res.files.discovered), res.files.loaded...))

	return nil
}

// applyPendingWrites persists the config-file edits the load pipeline
// requested. It never reloads: the caller is mid-load and already holds
// writeMu. Failures are logged and otherwise ignored, since the in-memory
// config already reflects the edit and the next load will retry it.
func (s *ConfigStore) applyPendingWrites(writes []pendingWrite) {
	for _, w := range writes {
		if err := s.removeConfigFieldNoReload(w.Scope, w.Remove); err != nil {
			slog.Warn("Failed to persist config change made while loading", "key", w.Remove, "error", err)
		}
	}
}

// autoReload conditionally reloads config from disk after writes.
// It returns nil (no error) for expected skip cases: when auto-reload is
// disabled during load/reload flows, or when working directory is not set
// (e.g., during testing). Only actual reload failures return an error.
func (s *ConfigStore) autoReload(ctx context.Context) error {
	if s.workingDir == "" {
		return nil // Expected skip: working directory not set
	}
	// Skip while Load builds the initial config: the write came from the
	// load itself (an OAuth refresh during the catalog fetch), whose result
	// Load publishes anyway, and reloading would re-enter that fetch.
	if s.loading.Load() {
		return nil
	}
	// Skip if a reload is already in progress. This handles both
	// concurrent auto-reloads after parallel writes and re-entrant
	// calls from writes made during a reload (e.g. an OAuth token refresh).
	//
	// Note: if a write completes after the in-progress reload has
	// already read the config file, that write won't be reflected in
	// memory until the next reload. This is acceptable because writes
	// are rare and the next user action or file-watch tick will pick
	// up the change. Callers that need guaranteed fresh state after a
	// write should call ReloadFromDisk explicitly.
	if !s.writeMu.TryLock() {
		return nil
	}
	defer s.writeMu.Unlock()
	return s.reloadFromDiskLocked(ctx)
}

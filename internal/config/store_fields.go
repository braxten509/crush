package config

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/charmbracelet/crush/internal/lock"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// configLockDeadline bounds how long lockConfig waits for the
// cross-process flock before giving up. A few seconds is plenty for
// honest contention; longer suggests something is wedged.
const configLockDeadline = 5 * time.Second

// lockConfig acquires both the in-process mutex and a cross-process flock
// on the config file for the given scope. Callers that need to do I/O
// between reading and writing (e.g. an HTTP token exchange) must use
// lockConfig explicitly rather than atomicWrite.
//
// The returned release function drops both locks. Callers must call it
// as soon as the file access is complete — no I/O should be performed
// while the lock is held.
func (s *ConfigStore) lockConfig(scope Scope) (func(), error) {
	s.mu.Lock()
	path, err := s.configPath(scope)
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		s.mu.Unlock()
		return nil, fmt.Errorf("create config directory: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), configLockDeadline)
	defer cancel()
	release, err := lock.File(ctx, path+".lock")
	if err != nil {
		s.mu.Unlock()
		return nil, fmt.Errorf("acquire config lock: %w", err)
	}
	return func() {
		release()
		s.mu.Unlock()
	}, nil
}

// atomicWrite handles the lock-read-transform-write-unlock cycle for
// config file mutations. The fn callback receives the current file
// contents (raw bytes, or {} if the file is missing) and must return the
// new contents. fn must be pure — no I/O, no network calls.
func (s *ConfigStore) atomicWrite(scope Scope, fn func(current []byte) ([]byte, error)) error {
	unlock, err := s.lockConfig(scope)
	if err != nil {
		return err
	}
	defer unlock()

	path, err := s.configPath(scope)
	if err != nil {
		return err
	}

	data, err := readFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			data = []byte("{}")
		} else {
			return fmt.Errorf("read config file: %w", err)
		}
	}

	newData, err := fn(data)
	if err != nil {
		return err
	}

	return atomicWriteFile(path, newData, 0o600)
}

// configPath returns the file path for the given scope.
func (s *ConfigStore) configPath(scope Scope) (string, error) {
	switch scope {
	case ScopeWorkspace:
		if s.workspacePath == "" {
			return "", ErrNoWorkspaceConfig
		}
		return s.workspacePath, nil
	default:
		return s.globalDataPath, nil
	}
}

// HasConfigField checks whether a key exists in the config file for the given
// scope.
func (s *ConfigStore) HasConfigField(scope Scope, key string) bool {
	path, err := s.configPath(scope)
	if err != nil {
		return false
	}
	data, err := readFile(path)
	if err != nil {
		return false
	}
	return gjson.Get(string(data), key).Exists()
}

// SetConfigField sets a key/value pair in the config file for the given scope.
// After a successful write, it automatically reloads config to keep in-memory
// state fresh.
func (s *ConfigStore) SetConfigField(scope Scope, key string, value any) error {
	return s.SetConfigFields(scope, map[string]any{key: value})
}

// SetConfigFields sets multiple key/value pairs in the config file for the
// given scope in a single write, then reloads in-memory state from disk.
//
// Use this for arbitrary external edits where the in-memory effect of the
// change is not known ahead of time. The typed mutators (which know exactly
// what changed) go through update instead and skip the reload.
//
// The write is protected by an in-process mutex and a cross-process flock
// to prevent races between concurrent writers in different processes.
func (s *ConfigStore) SetConfigFields(scope Scope, kv map[string]any) error {
	if err := s.writeConfigFields(scope, kv); err != nil {
		return err
	}
	// Auto-reload to keep in-memory state fresh after config edits.
	// We use context.Background() since this is an internal operation that
	// shouldn't be cancelled by user context.
	if err := s.autoReload(context.Background()); err != nil {
		// Log warning but don't fail the write - disk is already updated.
		slog.Warn("Config file updated but failed to reload in-memory state", "error", err)
	}
	return nil
}

// writeConfigFields persists key/value pairs to the config file. It does not
// touch in-memory config state or the staleness snapshot: callers either
// reload (SetConfigFields, whose reload recaptures the snapshot) or have
// already published an updated clone and capture the snapshot themselves
// (update). Both of those run under writeMu, which is what keeps the
// snapshot map free of concurrent writers.
func (s *ConfigStore) writeConfigFields(scope Scope, kv map[string]any) error {
	// Sort keys for deterministic output regardless of map iteration
	// order. This also ensures consistent results when callers pass
	// overlapping JSONPath keys (e.g. "a" and "a.b").
	keys := make([]string, 0, len(kv))
	for k := range kv {
		keys = append(keys, k)
	}
	slices.Sort(keys)

	return s.atomicWrite(scope, func(data []byte) ([]byte, error) {
		v := string(data)
		for _, key := range keys {
			var sErr error
			if v, sErr = sjson.Set(v, key, kv[key]); sErr != nil {
				return nil, fmt.Errorf("failed to set config field %s: %w", key, sErr)
			}
		}
		return []byte(v), nil
	})
}

// mutateInMemory applies a copy-on-write change to the config without
// persisting. Under writeMu it clones the live config, lets mutate edit the
// clone, and publishes it. This is the single primitive every in-memory
// config change goes through, so a published Config is never mutated in
// place and readers always see a consistent snapshot.
func (s *ConfigStore) mutateInMemory(mutate func(*Config)) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	nc := s.Config().cloneForWrite()
	mutate(nc)
	s.setConfig(nc)
}

// update applies a copy-on-write change and persists the reported fields.
// mutate edits the clone and returns the JSON-path fields to write to disk;
// because the clone already reflects the change, no reload is needed.
// Returning an empty map publishes the clone without a disk write.
func (s *ConfigStore) update(scope Scope, mutate func(*Config) map[string]any) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.updateLocked(scope, mutate)
}

// updateLocked is the lock-free core of update. Caller must hold writeMu.
func (s *ConfigStore) updateLocked(scope Scope, mutate func(*Config) map[string]any) error {
	nc := s.Config().cloneForWrite()
	fields := mutate(nc)
	s.setConfig(nc)
	if len(fields) == 0 {
		return nil
	}
	if err := s.writeConfigFields(scope, fields); err != nil {
		return err
	}
	// Refresh the staleness snapshot so the file watcher does not treat
	// our own write as an external change. Safe to touch the snapshot map
	// here because we hold writeMu.
	if path, err := s.configPath(scope); err == nil {
		s.captureStalenessSnapshot(append(slices.Clone(s.loadedPaths), path))
	}
	return nil
}

// OverrideHelperModel sets the helper model in memory only, without
// persisting. It is for per-run overrides (crush run --small-model) that
// must not be written to the user's config file.
func (s *ConfigStore) OverrideHelperModel(model SelectedModel) {
	s.mutateInMemory(func(c *Config) {
		if c.Models == nil {
			c.Models = make(map[SelectedModelType]SelectedModel)
		}
		c.Models[SelectedModelTypeSmall] = model
		s.pinPreferredModelLocked(SelectedModelTypeSmall, model)
	})
}

// pinPreferredModelLocked records a model choice made in this instance so
// that a later config reload cannot replace it with a choice made
// somewhere else. Several Crush instances share one global config file, so
// a reload triggered by an unrelated write (a token refresh, say) would
// otherwise import whichever model a sibling instance last selected and
// switch models out from under the user mid-session.
//
// Caller must hold writeMu.
func (s *ConfigStore) pinPreferredModelLocked(modelType SelectedModelType, model SelectedModel) {
	if s.overrides.Models == nil {
		s.overrides.Models = make(map[SelectedModelType]SelectedModel)
	}
	s.overrides.Models[modelType] = model
}

// RemoveConfigField removes a key from the config file for the given scope.
// After a successful write, it automatically reloads config to keep in-memory
// state fresh.
//
// The write is protected by an in-process mutex and a cross-process flock.
func (s *ConfigStore) RemoveConfigField(scope Scope, key string) error {
	if err := s.removeConfigFieldNoReload(scope, key); err != nil {
		return err
	}

	if err := s.autoReload(context.Background()); err != nil {
		slog.Warn("Config file updated but failed to reload in-memory state", "error", err)
	}

	return nil
}

// ConfigEditGuard judges a config file edit inside the locked
// read-transform-write, from the file's contents before and after it. An
// error refuses the edit: nothing is written and the error is returned.
type ConfigEditGuard func(before, after []byte) error

// SetConfigFieldGuarded is [ConfigStore.SetConfigField] with guard judging
// the edit under the same config locks as the write, so no other writer
// can change the file between the judgment and the write.
func (s *ConfigStore) SetConfigFieldGuarded(scope Scope, key string, value any, guard ConfigEditGuard) error {
	return s.guardedEdit(scope, guard, func(v string) (string, error) {
		v, err := sjson.Set(v, key, value)
		if err != nil {
			return "", fmt.Errorf("failed to set config field %s: %w", key, err)
		}
		return v, nil
	})
}

// RemoveConfigFieldGuarded is [ConfigStore.RemoveConfigField] with guard
// judging the edit as in [ConfigStore.SetConfigFieldGuarded].
func (s *ConfigStore) RemoveConfigFieldGuarded(scope Scope, key string, guard ConfigEditGuard) error {
	return s.guardedEdit(scope, guard, func(v string) (string, error) {
		v, err := sjson.Delete(v, key)
		if err != nil {
			return "", fmt.Errorf("failed to delete config field %s: %w", key, err)
		}
		return v, nil
	})
}

// guardedEdit applies edit to scope's file in one locked
// read-transform-write, asking guard first, then reloads as
// SetConfigFields does.
func (s *ConfigStore) guardedEdit(scope Scope, guard ConfigEditGuard, edit func(string) (string, error)) error {
	err := s.atomicWrite(scope, func(data []byte) ([]byte, error) {
		after, err := edit(string(data))
		if err != nil {
			return nil, err
		}
		if err := guard(data, []byte(after)); err != nil {
			return nil, err
		}
		return []byte(after), nil
	})
	if err != nil {
		return err
	}
	if err := s.autoReload(context.Background()); err != nil {
		slog.Warn("Config file updated but failed to reload in-memory state", "error", err)
	}
	return nil
}

// removeConfigFieldNoReload deletes a key from the config file for the
// given scope without touching in-memory state.
func (s *ConfigStore) removeConfigFieldNoReload(scope Scope, key string) error {
	return s.atomicWrite(scope, func(data []byte) ([]byte, error) {
		v, sErr := sjson.Delete(string(data), key)
		if sErr != nil {
			return nil, fmt.Errorf("failed to delete config field %s: %w", key, sErr)
		}
		return []byte(v), nil
	})
}

// SetHelperModel sets the helper model (titles, branch summaries, web
// fetches) and persists it to the config file at the given scope. The model
// and the helper's recent-models list are written together in a single
// config write.
//
// The write skips the full disk reparse/reload (which would rebuild the
// provider catalog and agents on every model switch and dominate selection
// latency); agents are refreshed separately by the caller (see
// UpdateAgentModel).
func (s *ConfigStore) SetHelperModel(scope Scope, model SelectedModel) error {
	return s.update(scope, func(c *Config) map[string]any {
		return s.helperModelFields(c, model)
	})
}

// RecordChatPick adds a model picked for a chat to the chats' recent list
// and remembers its effort. No model is set: the chat owns it.
func (s *ConfigStore) RecordChatPick(model SelectedModel) error {
	return s.update(ScopeGlobal, func(c *Config) map[string]any {
		return pickFields(c, SelectedModelTypeChat, model, map[string]any{})
	})
}

// helperModelFields builds the fields map for persisting the helper model.
// Shared between SetHelperModel and direct updateLocked callers (e.g.
// Load). Caller must hold writeMu.
func (s *ConfigStore) helperModelFields(c *Config, model SelectedModel) map[string]any {
	if c.Models == nil {
		c.Models = make(map[SelectedModelType]SelectedModel)
	}
	c.Models[SelectedModelTypeSmall] = model
	s.pinPreferredModelLocked(SelectedModelTypeSmall, model)
	return pickFields(c, SelectedModelTypeSmall, model, map[string]any{
		fmt.Sprintf("models.%s", SelectedModelTypeSmall): model,
	})
}

// pickFields adds a pick's recent-list entry and remembered effort to
// fields. Caller must hold writeMu.
func pickFields(c *Config, modelType SelectedModelType, model SelectedModel, fields map[string]any) map[string]any {
	if updated, changed := nextRecentModels(c, modelType, model); changed {
		if c.RecentModels == nil {
			c.RecentModels = make(map[SelectedModelType][]SelectedModel)
		}
		c.RecentModels[modelType] = updated
		fields[fmt.Sprintf("recent_models.%s", modelType)] = updated
	}
	// The whole map is written: model IDs hold dots, which a key path
	// would split.
	if key := modelEffortKey(model.Provider, model.Model); key != "" && model.ReasoningEffort != "" && c.ModelEfforts[key] != model.ReasoningEffort {
		efforts := maps.Clone(c.ModelEfforts)
		if efforts == nil {
			efforts = map[string]string{}
		}
		efforts[key] = model.ReasoningEffort
		c.ModelEfforts = efforts
		fields["model_efforts"] = efforts
	}
	return fields
}

// SetCompactMode sets the compact mode setting and persists it.
func (s *ConfigStore) SetCompactMode(scope Scope, enabled bool) error {
	return s.update(scope, func(c *Config) map[string]any {
		c.ensureTUI().CompactMode = enabled
		return map[string]any{"options.tui.compact_mode": enabled}
	})
}

// SetTransparentBackground sets the transparent background setting and persists it.
func (s *ConfigStore) SetTransparentBackground(scope Scope, enabled bool) error {
	return s.update(scope, func(c *Config) map[string]any {
		c.ensureTUI().Transparent = &enabled
		return map[string]any{"options.tui.transparent": enabled}
	})
}

// nextRecentModels computes the recent-models list for the given type
// after recording the supplied model at the front, operating on the
// provided config without persisting anything. It returns the new slice
// and whether it differs from cfg's current list. Callers fold the result
// into a clone they are about to publish.
func nextRecentModels(cfg *Config, modelType SelectedModelType, model SelectedModel) ([]SelectedModel, bool) {
	if model.Provider == "" || model.Model == "" {
		return nil, false
	}

	eq := func(a, b SelectedModel) bool {
		return a.Provider == b.Provider && a.Model == b.Model
	}

	entry := SelectedModel{
		Provider: model.Provider,
		Model:    model.Model,
	}

	current := cfg.RecentModels[modelType]
	withoutCurrent := slices.DeleteFunc(slices.Clone(current), func(existing SelectedModel) bool {
		return eq(existing, entry)
	})

	updated := append([]SelectedModel{entry}, withoutCurrent...)
	if len(updated) > maxRecentModelsPerType {
		updated = updated[:maxRecentModelsPerType]
	}

	if slices.EqualFunc(current, updated, eq) {
		return current, false
	}

	return updated, true
}

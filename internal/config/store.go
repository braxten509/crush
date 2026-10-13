package config

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/env"
	"github.com/charmbracelet/crush/internal/oauth"
	"golang.org/x/sync/singleflight"
)

// RuntimeOverrides holds per-session settings that are never persisted to
// disk. They are applied on top of the loaded Config and survive only for
// the lifetime of the process (or workspace).
type RuntimeOverrides struct {
	SkipPermissionRequests bool
	// EnabledChannels lists the MCP servers opted in as channels for this
	// session (via the --channels flag). A server present in MCP config only
	// pushes channel events when it also appears here. Entries may be written
	// as "server:<name>" or as a bare "<name>".
	EnabledChannels []string
	// Models records the model choices made in this instance, whether
	// persisted or not. They are reapplied after a config reload so that a
	// selection made here always outranks whatever the shared config file
	// happens to hold — see pinPreferredModelLocked.
	Models map[SelectedModelType]SelectedModel
}

// ConfigStore is the single entry point for all config access. It owns the
// pure-data Config, runtime state (working directory, resolver, known
// providers), and persistence to both global and workspace config files.
//
// mu serialises all config file mutations (SetConfigFields,
// RemoveConfigField, RefreshOAuthToken) to prevent both in-process
// goroutine races and, together with the shared lock.File, cross-process
// races on the config file.
//
// writeMu serialises every operation that produces a new in-memory Config:
// the typed copy-on-write mutators (SetCompactMode, UpdatePreferredModel,
// ...) and ReloadFromDisk. Typed mutators take Lock; autoReload takes
// TryLock so a write triggered re-entrantly during a reload (e.g. an OAuth
// token refresh) skips the nested reload instead of deadlocking. The load
// pipeline itself never writes mid-load: it returns pending writes that
// the store persists afterwards. This is what lets published Configs be
// treated as immutable: a mutator clones, mutates the clone, and swaps it
// in under writeMu rather than mutating the live Config in place.
type ConfigStore struct {
	config             *Config
	workingDir         string
	resolver           VariableResolver
	globalDataPath     string   // ~/.local/share/crush/crush.json
	workspacePath      string   // .crush/crush.json
	loadedPaths        []string // config files that were successfully loaded
	knownProviders     []catwalk.Provider
	overrides          RuntimeOverrides
	trackedConfigPaths []string                // unique, normalized config file paths
	snapshots          map[string]fileSnapshot // path -> snapshot at last capture

	// configMu guards the config pointer field against concurrent
	// readers (Config) and the writeMu-serialised swap (setConfig). It
	// protects the pointer word only; the pointed-to Config is treated
	// as immutable once published, since both reloads and typed mutators
	// build a fresh Config rather than mutating the live one.
	configMu sync.RWMutex

	mu      sync.Mutex   // serialises config file writes
	writeMu sync.RWMutex // serialises in-memory config production (mutators + reload); RLock for readers

	// refreshSF collapses concurrent in-process OAuth refreshes for the
	// same provider into a single attempt. Combined with the per-provider
	// cross-process refresh lock, it ensures only one token exchange runs
	// at a time. See RefreshOAuthToken.
	refreshSF singleflight.Group

	// exchangeToken performs the provider-specific OAuth token exchange.
	// It is a field so tests can substitute a fake exchange without making
	// real network calls. Production code leaves it nil, and exchange falls
	// back to the real provider clients.
	exchangeToken func(ctx context.Context, providerID, refreshToken string) (*oauth.Token, error)

	// authSignalMu guards authSignals, which maps provider IDs to
	// channels that WaitForTokenChange blocks on. SignalAuthComplete
	// closes the channel to unblock waiters; a new channel is created
	// on the next wait.
	authSignalMu sync.Mutex
	authSignals  map[string]chan struct{}

	// debug and outsideWorktree are the load-time inputs a reload must
	// reapply: the --debug flag, and whether Crush runs outside a git
	// working tree (checked once, as it shells out to git).
	debug           bool
	outsideWorktree bool

	// loading is set while Load builds the initial config. Writes made
	// meanwhile (an OAuth refresh triggered by the catalog fetch) must not
	// auto-reload: a reload re-enters the provider catalog fetch that is
	// still running, which deadlocks.
	loading atomic.Bool
}

// Config returns the pure-data config struct (read-only after load).
//
// The pointer read is guarded by configMu so it can never tear against
// the reload swap in reloadFromDiskLocked. Reloads build a brand-new
// Config and swap it in rather than mutating the live one, so holding the
// returned pointer stays safe even across a concurrent reload — the reader
// keeps reading its (now immutable) snapshot.
func (s *ConfigStore) Config() *Config {
	s.configMu.RLock()
	defer s.configMu.RUnlock()
	return s.config
}

// setConfig atomically swaps the active config pointer under configMu.
// Used by the reload path; in-place field mutators leave the pointer
// untouched and run under mu instead.
func (s *ConfigStore) setConfig(cfg *Config) {
	s.configMu.Lock()
	defer s.configMu.Unlock()
	s.config = cfg
}

// WorkingDir returns the current working directory.
func (s *ConfigStore) WorkingDir() string {
	return s.workingDir
}

// Resolver returns the variable resolver.
func (s *ConfigStore) Resolver() VariableResolver {
	s.writeMu.RLock()
	defer s.writeMu.RUnlock()
	return s.resolver
}

// Resolve resolves a variable reference using the configured resolver.
func (s *ConfigStore) Resolve(key string) (string, error) {
	s.writeMu.RLock()
	r := s.resolver
	s.writeMu.RUnlock()
	if r == nil {
		return "", fmt.Errorf("no variable resolver configured")
	}
	return r.ResolveValue(key)
}

// KnownProviders returns the list of known providers.
func (s *ConfigStore) KnownProviders() []catwalk.Provider {
	s.writeMu.RLock()
	defer s.writeMu.RUnlock()
	return s.knownProviders
}

// SetupAgents configures the coder and task agents on the config.
func (s *ConfigStore) SetupAgents() {
	s.Config().SetupAgents()
}

// Overrides returns the runtime overrides for this store.
func (s *ConfigStore) Overrides() *RuntimeOverrides {
	s.writeMu.RLock()
	defer s.writeMu.RUnlock()
	return &s.overrides
}

// LoadedPaths returns the config file paths that were successfully loaded.
func (s *ConfigStore) LoadedPaths() []string {
	s.writeMu.RLock()
	defer s.writeMu.RUnlock()
	return slices.Clone(s.loadedPaths)
}

// NewTestStore creates a ConfigStore for testing purposes.
func NewTestStore(cfg *Config, loadedPaths ...string) *ConfigStore {
	return &ConfigStore{
		config:      cfg,
		loadedPaths: loadedPaths,
		resolver:    NewShellVariableResolver(env.New()),
	}
}

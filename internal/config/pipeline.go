package config

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/env"
	"github.com/charmbracelet/crush/internal/fsext"
	"github.com/charmbracelet/crush/internal/shellconfig"
	"github.com/qjebbs/go-jsons"
	"github.com/tidwall/gjson"
)

// buildParams are the inputs to buildConfig. Load and ReloadFromDisk run
// the same pipeline and differ only in what they pass here.
type buildParams struct {
	workingDir string
	// dataDir forces the data directory; empty derives it from the config
	// files and the working directory.
	dataDir string
	// debug forces Options.Debug on (the --debug flag). It is applied
	// before the workspace overlay, which may still override it.
	debug bool
	// outsideWorktree limits file walks because the process is not running
	// inside a git working tree.
	outsideWorktree bool
	// modelOverrides are the model choices made in this instance. They are
	// laid over the files so a reload cannot swap the user's model for one
	// a sibling instance selected in the shared config file.
	modelOverrides map[SelectedModelType]SelectedModel
	// refreshHyper lets the provider catalog fetch retry Hyper on a 401.
	refreshHyper HyperTokenRefresher
	// onMerged, when set, receives the merged config and the files it came
	// from just before the provider catalog loads. Load uses it to publish
	// the in-progress config, which refreshHyper and onCatalog read
	// through the store.
	onMerged func(cfg *Config, files configFiles)
	// onCatalog, when set, runs after the provider catalog loads and
	// returns the config to continue with, which may be a replacement.
	onCatalog func(cfg *Config) *Config
	// reload selects the reload wording for errors and log messages.
	reload bool
}

// configFiles records where a built config came from.
type configFiles struct {
	// discovered lists every config path looked at, loaded or not, so a
	// file created later is noticed by the staleness check.
	discovered []string
	// loaded lists the files that contributed to the config.
	loaded []string
	// workspace is the workspace config file (<data dir>/crush.json).
	workspace string
}

// buildResult is everything buildConfig produced. The caller publishes it
// on the store.
type buildResult struct {
	cfg            *Config
	files          configFiles
	resolver       VariableResolver
	knownProviders []catwalk.Provider
	// pending are config-file edits the caller must persist. They are set
	// even when buildConfig fails part-way.
	pending []pendingWrite
	// configured reports whether any provider is usable. When false, no
	// models were resolved and no agents were set up.
	configured bool
	// models is the resolved model selection, valid when configured.
	models resolvedModels
}

// buildConfig runs the whole config load pipeline: it reads and merges the
// config files, applies defaults and the workspace overlay, validates
// hooks, loads the provider catalog, configures providers, and resolves
// the selected models. It never writes to disk itself; edits it decides
// on come back as pending writes.
//
// On error the returned result is still non-nil when it carries pending
// writes the caller should persist.
func buildConfig(ctx context.Context, p buildParams) (*buildResult, error) {
	// Migrate deprecated disable_notifications before loading config.
	migrateDisableNotifications()

	cfg, files, err := readConfig(ctx, p)
	if err != nil {
		return nil, err
	}

	if p.onMerged != nil {
		p.onMerged(cfg, files)
	}
	providers, err := p.loadCatalog(cfg)
	if err != nil {
		return nil, err
	}
	if p.onCatalog != nil {
		cfg = p.onCatalog(cfg)
	}

	vars := env.New()
	resolver := NewShellVariableResolver(vars)
	res := &buildResult{
		cfg:            cfg,
		files:          files,
		resolver:       resolver,
		knownProviders: providers,
	}

	// Apply top-level env vars before configuring providers so variables
	// like AWS_PROFILE are visible to the AWS SDK credential chain.
	cfg.applyEnv(resolver)

	res.pending, err = cfg.configureProviders(ctx, vars, resolver, providers)
	if err != nil {
		return res, p.errorf("failed to configure providers", "failed to configure providers during reload", err)
	}

	if !cfg.IsConfigured() {
		slog.Warn(p.pick("No providers configured", "No providers configured after reload"))
		return res, nil
	}

	res.models, err = resolveSelectedModels(cfg, providers)
	if err != nil {
		return res, p.errorf("failed to configure selected models", "failed to configure selected models during reload", err)
	}
	cfg.Models[SelectedModelTypeLarge] = res.models.Large
	cfg.Models[SelectedModelTypeSmall] = res.models.Small
	cfg.SetupAgents()
	res.configured = true
	return res, nil
}

// readConfig reads and merges the config files, then applies defaults,
// the workspace overlay, hook validation, environment-driven defaults and
// the instance's model overrides: everything that needs no provider
// catalog.
func readConfig(ctx context.Context, p buildParams) (*Config, configFiles, error) {
	var files configFiles
	files.discovered = lookupConfigs(p.workingDir)
	cfg, loaded, err := loadFromConfigPaths(ctx, files.discovered)
	if err != nil {
		if p.reload {
			return nil, files, fmt.Errorf("failed to reload config: %w", err)
		}
		return nil, files, fmt.Errorf("failed to load config from paths %v: %w", files.discovered, err)
	}
	files.loaded = loaded

	cfg.setDefaults(p.workingDir, p.dataDir)
	if p.debug {
		cfg.Options.Debug = true
	}

	// Load workspace config last so it has highest priority.
	files.workspace = filepath.Join(cfg.Options.DataDirectory, fmt.Sprintf("%s.json", appName))
	merged, err := cfg.applyWorkspaceOverlay(p.workingDir, files.workspace)
	if err != nil {
		return nil, files, err
	}
	if merged {
		files.loaded = append(files.loaded, files.workspace)
	}

	// Validate hooks after all config merging is complete so workspace
	// hooks are validated too.
	if err := cfg.ValidateHooks(); err != nil {
		return nil, files, p.errorf("invalid hook configuration", "invalid hook configuration on reload", err)
	}

	cfg.applyEnvironmentDefaults(p.outsideWorktree, !p.reload)

	// Reapply model choices made in this instance. The global config file is
	// shared, so it may now name a model a sibling instance selected; a
	// reload triggered by an unrelated write must not swap the user's model
	// mid-session. An external edit to the config still takes effect for any
	// model type this instance never chose.
	maps.Copy(cfg.Models, p.modelOverrides)
	return cfg, files, nil
}

// applyWorkspaceOverlay merges the workspace config file at path over c,
// keeping the data directory setDefaults already resolved. It reports
// whether the file was merged. A missing or empty file is skipped, and so
// is one that fails to merge; invalid JSON is an error.
func (c *Config) applyWorkspaceOverlay(workingDir, path string) (bool, error) {
	wsData, err := readFile(path)
	if err != nil || len(wsData) == 0 {
		return false, nil
	}
	if !json.Valid(wsData) {
		return false, fmt.Errorf("invalid JSON in config file %s", path)
	}
	merged, err := loadFromBytes(append([][]byte{mustMarshalConfig(c)}, wsData))
	if err != nil {
		return false, nil
	}
	// Preserve defaults that setDefaults already applied.
	dataDir := c.Options.DataDirectory
	*c = *merged
	c.setDefaults(workingDir, dataDir)
	return true, nil
}

// applyEnvironmentDefaults fills in defaults that depend on where Crush
// runs rather than on the config files: bounded file walks outside a git
// working tree, and transparency in Apple Terminal. Explicit settings
// win. logWarnings controls whether the adjustments are logged, so a
// reload does not repeat what startup already reported.
func (c *Config) applyEnvironmentDefaults(outsideWorktree, logWarnings bool) {
	if outsideWorktree {
		const depth = 2
		const items = 100
		if logWarnings {
			slog.Warn("No git repository detected in working directory, will limit file walk operations", "depth", depth, "items", items)
		}
		assignIfNil(&c.Tools.Ls.MaxDepth, depth)
		assignIfNil(&c.Tools.Ls.MaxItems, items)
		assignIfNil(&c.Options.TUI.Completions.MaxDepth, depth)
		assignIfNil(&c.Options.TUI.Completions.MaxItems, items)
	}

	if isAppleTerminal() {
		if logWarnings {
			slog.Warn("Detected Apple Terminal, enabling transparent mode")
		}
		assignIfNil(&c.Options.TUI.Transparent, true)
	}
}

// loadCatalog loads the known providers from Catwalk and Hyper. A failed
// refresh still yields the cached or embedded catalog, so only an empty
// list is fatal: starting up without providers is worse than starting up
// with slightly stale ones.
func (p buildParams) loadCatalog(cfg *Config) ([]catwalk.Provider, error) {
	var refreshers []HyperTokenRefresher
	if p.refreshHyper != nil {
		refreshers = append(refreshers, p.refreshHyper)
	}
	providers, err := Providers(cfg, refreshers...)
	if err != nil {
		if len(providers) == 0 {
			if p.reload {
				return nil, fmt.Errorf("failed to load providers during reload: %w", err)
			}
			return nil, err
		}
		slog.Warn(p.pick("Continuing with the previously known providers", "Reload continuing with the previously known providers"), "error", err)
	}
	return providers, nil
}

// pick returns the load or the reload variant of a message.
func (p buildParams) pick(load, reload string) string {
	if p.reload {
		return reload
	}
	return load
}

// errorf wraps err with the load or the reload variant of a message.
func (p buildParams) errorf(load, reload string, err error) error {
	return fmt.Errorf("%s: %w", p.pick(load, reload), err)
}

// mustMarshalConfig marshals the config to JSON bytes, returning empty JSON on
// error.
func mustMarshalConfig(cfg *Config) []byte {
	data, err := json.Marshal(cfg)
	if err != nil {
		return []byte("{}")
	}
	return data
}

// lookupConfigs searches config files starting at cwd and walking up
// through the current project. The upward walk stops at the git
// working tree root when one can be detected, otherwise at cwd itself,
// so an unrelated crush.json placed above the project is never picked
// up. Global user-level config locations are always included
// regardless of the boundary.
func lookupConfigs(cwd string) []string {
	// Prepend global user config and machine-owned data JSON. Only the user
	// config directory contributes a crushrc; the data directory is writable
	// machine state and must never be executed as Bash. Missing files are
	// skipped when loaded.
	configPaths := []string{
		systemConfigPath,
		GlobalConfig(),
		shellConfigSibling(GlobalConfig()),
		GlobalConfigData(),
	}

	// Ordered high-to-low priority within a directory. LookupBounded returns
	// matches in this order, and the later reverse + merge make the earliest
	// listed name win on conflict. So: .crushrc beats crushrc, both beat the
	// JSON configs, and .crush.json beats crush.json.
	configNames := []string{
		"." + appName + "rc",
		appName + "rc",
		"." + appName + ".json",
		appName + ".json",
	}

	foundConfigs, err := fsext.LookupBounded(cwd, projectBoundary(cwd), configNames...)
	if err != nil {
		// returns at least default configs
		return configPaths
	}

	// reverse order so last config has more priority
	slices.Reverse(foundConfigs)

	return append(configPaths, foundConfigs...)
}

func loadFromConfigPaths(ctx context.Context, configPaths []string) (*Config, []string, error) {
	var configs [][]byte
	var loaded []string

	// Track directories that have both crush.json and crushrc to warn
	// about potential confusion, along with the top-level keys each
	// defines so we can report conflicts.
	jsonDirKeys := make(map[string]map[string]bool)
	shDirKeys := make(map[string]map[string]bool)

	for _, path := range configPaths {
		if path == "" {
			continue
		}
		data, err := readFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, nil, fmt.Errorf("failed to open config file %s: %w", path, err)
		}
		if len(data) == 0 {
			continue
		}

		dir := filepath.Dir(path)
		if isShellConfig(path) {
			jsonBytes, err := shellconfig.LoadShellConfig(ctx, path, data)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to load shell config %s: %w", path, err)
			}
			if len(jsonBytes) > 0 {
				if !json.Valid(jsonBytes) {
					return nil, nil, fmt.Errorf("shell config %s produced invalid JSON", path)
				}
				addTopLevelKeys(shDirKeys, dir, jsonBytes)
				configs = append(configs, jsonBytes)
				loaded = append(loaded, path)
			}
		} else {
			if !json.Valid(data) {
				return nil, nil, fmt.Errorf("invalid JSON in config file %s", path)
			}
			addTopLevelKeys(jsonDirKeys, dir, data)
			configs = append(configs, data)
			loaded = append(loaded, path)
		}
	}

	// Warn if both a JSON config and a crushrc exist in the same directory
	// and define overlapping top-level keys. Disjoint coexistence is
	// intentional and not worth warning about.
	for dir, jKeys := range jsonDirKeys {
		sKeys, ok := shDirKeys[dir]
		if !ok {
			continue
		}
		var conflicts []string
		for k := range jKeys {
			if sKeys[k] {
				conflicts = append(conflicts, k)
			}
		}
		if len(conflicts) > 0 {
			slices.Sort(conflicts)
			slog.Warn("Found both a JSON config and a crushrc in the same directory; merging with crushrc taking precedence",
				"dir", dir, "conflicting_keys", strings.Join(conflicts, ", "))
		}
	}

	cfg, err := loadFromBytes(configs)
	if err != nil {
		return nil, nil, err
	}
	return cfg, loaded, nil
}

// addTopLevelKeys records the top-level JSON keys present in data into the
// set for dir.
func addTopLevelKeys(m map[string]map[string]bool, dir string, data []byte) {
	keys := m[dir]
	if keys == nil {
		keys = make(map[string]bool)
		m[dir] = keys
	}
	gjson.ParseBytes(data).ForEach(func(key, _ gjson.Result) bool {
		keys[key.String()] = true
		return true
	})
}

func loadFromBytes(configs [][]byte) (*Config, error) {
	if len(configs) == 0 {
		return &Config{}, nil
	}

	data, err := jsons.Merge(configs)
	if err != nil {
		return nil, err
	}
	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}
	return &config, nil
}

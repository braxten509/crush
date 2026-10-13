package config

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/agent/hyper"
	"github.com/charmbracelet/crush/internal/csync"
	"github.com/charmbracelet/crush/internal/discover"
	"github.com/charmbracelet/crush/internal/env"
	"github.com/charmbracelet/crush/internal/filepathext"
	"github.com/charmbracelet/crush/internal/fsext"
	"github.com/charmbracelet/crush/internal/home"
	"github.com/charmbracelet/crush/internal/shellconfig"
	powernapConfig "github.com/charmbracelet/x/powernap/pkg/config"
	"github.com/qjebbs/go-jsons"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const defaultCatwalkURL = "https://catwalk.charm.land"

// Load loads the configuration from the default paths and returns a
// ConfigStore that owns both the pure-data Config and all runtime state.
func Load(workingDir, dataDir string, debug bool) (*ConfigStore, error) {
	store := &ConfigStore{
		workingDir:      workingDir,
		globalDataPath:  GlobalConfigData(),
		debug:           debug,
		outsideWorktree: !isInsideWorktree(),
	}

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
	})

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

// persistModelFallbacks writes the model selections that resolution had
// to correct back to the global config. Caller must hold writeMu.
func (s *ConfigStore) persistModelFallbacks(resolved resolvedModels) error {
	if resolved.LargeFallback {
		if err := s.updateLocked(ScopeGlobal, func(c *Config) map[string]any {
			return s.updatePreferredModelFields(c, SelectedModelTypeLarge, resolved.Large)
		}); err != nil {
			return fmt.Errorf("failed to update preferred large model: %w", err)
		}
	}
	if resolved.SmallFallback {
		if err := s.updateLocked(ScopeGlobal, func(c *Config) map[string]any {
			return s.updatePreferredModelFields(c, SelectedModelTypeSmall, resolved.Small)
		}); err != nil {
			return fmt.Errorf("failed to update preferred small model: %w", err)
		}
	}
	return nil
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

func PushPopCrushEnv() func() {
	var found []string
	for _, ev := range os.Environ() {
		if strings.HasPrefix(ev, "CRUSH_") {
			pair := strings.SplitN(ev, "=", 2)
			if len(pair) != 2 {
				continue
			}
			found = append(found, strings.TrimPrefix(pair[0], "CRUSH_"))
		}
	}
	backups := make(map[string]string)
	for _, ev := range found {
		backups[ev] = os.Getenv(ev)
	}

	for _, ev := range found {
		os.Setenv(ev, os.Getenv("CRUSH_"+ev))
	}

	restore := func() {
		for k, v := range backups {
			os.Setenv(k, v)
		}
	}
	return restore
}

// pendingWrite is a config-file edit the load pipeline decided on but left
// for the store to persist once loading is done, so that building a Config
// never writes to disk part-way through.
type pendingWrite struct {
	Scope Scope
	// Remove is the JSON path to delete from the scope's config file.
	Remove string
}

// providerSetup is the state configureProviders threads through its
// phases.
type providerSetup struct {
	cfg      *Config
	env      env.Env
	resolver VariableResolver
	// catalog is the full provider catalog. Custom-provider model
	// discovery enriches models from it even when default providers are
	// disabled.
	catalog []catwalk.Provider
	// known holds the IDs of the catalog providers configured here; every
	// other provider in the config is a custom one.
	known   map[string]bool
	pending []pendingWrite
}

// configureProviders resolves the known and custom providers in c against
// the environment. Any config-file edits it decides on are returned as
// pending writes for the caller to persist; they are returned even when
// configuration fails part-way, matching the disk state an inline write
// would have left behind.
func (c *Config) configureProviders(ctx context.Context, env env.Env, resolver VariableResolver, knownProviders []catwalk.Provider) ([]pendingWrite, error) {
	restore := PushPopCrushEnv()
	defer restore()

	s := &providerSetup{
		cfg:      c,
		env:      env,
		resolver: resolver,
		catalog:  knownProviders,
		known:    make(map[string]bool),
	}

	// When disable_default_providers is enabled, skip all default/embedded
	// providers entirely. Users must fully specify any providers they want.
	// We skip to the custom provider validation loop which handles all
	// user-configured providers uniformly.
	if c.Options.DisableDefaultProviders {
		if len(s.catalog) == 0 {
			s.catalog = ModelCatalog()
		}
		knownProviders = nil
	}

	for _, p := range knownProviders {
		if err := s.configureKnownProvider(p); err != nil {
			return s.pending, err
		}
	}

	discovered := s.discoverCustomModels(ctx)

	c.addCLIProviders(env.Get("PATH"))

	if err := s.validateCustomProviders(discovered); err != nil {
		return s.pending, err
	}

	if c.Providers.Len() == 0 && c.Options.DisableDefaultProviders {
		return s.pending, fmt.Errorf("default providers are disabled and there are no custom providers are configured")
	}

	return s.pending, nil
}

// configureKnownProvider configures one catalog provider: it merges the
// user's overrides into the catalog entry, resolves headers, applies auth
// quirks, and keeps the provider only when its credential gate passes.
func (s *providerSetup) configureKnownProvider(p catwalk.Provider) error {
	id := string(p.ID)
	s.known[id] = true
	config, configExists := s.cfg.Providers.Get(id)
	// If the user configured a known provider we need to allow it to
	// override a couple of parameters.
	if configExists {
		p = applyProviderOverrides(p, config)
	}

	headers := map[string]string{}
	maps.Copy(headers, p.DefaultHeaders)
	maps.Copy(headers, config.ExtraHeaders)
	// Provider headers use the same error contract as MCP headers: a
	// failing $(...) aborts the provider load with a clear message, and a
	// header that resolves to the empty string (unset bare $VAR under
	// lenient nounset, $(echo), or literal "") is dropped from the
	// outgoing request.
	if err := resolveProviderHeaders(s.resolver, id, headers); err != nil {
		return err
	}
	prepared := prepareKnownProvider(p, config, headers)

	switch {
	case p.ID == catwalk.InferenceProviderAnthropic && config.OAuthToken != nil:
		// Claude Code subscription is not supported anymore. Remove to show onboarding.
		// The deletion is persisted by the caller once loading is done;
		// Providers.Del keeps the in-memory state consistent meanwhile.
		s.pending = append(s.pending, pendingWrite{Scope: ScopeGlobal, Remove: "providers.anthropic"})
		s.cfg.Providers.Del(id)
		return nil
	case p.ID == catwalk.InferenceProviderCopilot && config.OAuthToken != nil:
		prepared.SetupGitHubCopilot()
	}

	gate, ok := credentialGates[p.ID]
	if !ok {
		gate = apiKeyCredentials
	}
	if skip := gate(s, p, config, &prepared); skip != nil {
		// Only a provider the user configured is worth a warning; an
		// unconfigured catalog entry without credentials is the norm.
		if configExists {
			slog.Warn(skip.msg, skip.attrs...)
			s.cfg.Providers.Del(id)
		}
		return nil
	}
	s.cfg.Providers.Set(id, prepared)
	return nil
}

// applyProviderOverrides lays the user's base URL, API key and models over
// a catalog provider. User models come first and win on ID clashes; models
// without a name are named after their ID.
func applyProviderOverrides(p catwalk.Provider, config ProviderConfig) catwalk.Provider {
	if config.BaseURL != "" {
		p.APIEndpoint = config.BaseURL
	}
	if config.APIKey != "" {
		p.APIKey = config.APIKey
	}
	if len(config.Models) == 0 {
		return p
	}
	models := []catwalk.Model{}
	seen := make(map[string]bool)
	for _, model := range slices.Concat(config.Models, p.Models) {
		if seen[model.ID] {
			continue
		}
		seen[model.ID] = true
		if model.Name == "" {
			model.Name = model.ID
		}
		models = append(models, model)
	}
	p.Models = models
	return p
}

// prepareKnownProvider builds the provider config for a catalog provider.
// It starts from the user config so all user fields survive without
// explicit copying, then overlays the catwalk identity and endpoint fields
// (already merged with the user's overrides).
func prepareKnownProvider(p catwalk.Provider, config ProviderConfig, headers map[string]string) ProviderConfig {
	prepared := config
	prepared.ID = string(p.ID)
	prepared.Name = p.Name
	prepared.BaseURL = p.APIEndpoint
	prepared.APIKey = p.APIKey
	prepared.APIKeyTemplate = p.APIKey // Store original template for re-resolution
	prepared.Type = p.Type
	prepared.Models = p.Models
	prepared.ExtraHeaders = headers
	if prepared.ExtraParams == nil {
		prepared.ExtraParams = make(map[string]string)
	}
	return prepared
}

// resolveProviderHeaders expands every header value in place, dropping
// headers that resolve to the empty string.
func resolveProviderHeaders(resolver VariableResolver, providerID string, headers map[string]string) error {
	for k, v := range headers {
		resolved, err := resolver.ResolveValue(v)
		if err != nil {
			return fmt.Errorf("resolving provider %s header %q: %w", providerID, k, err)
		}
		if resolved == "" {
			delete(headers, k)
			continue
		}
		headers[k] = resolved
	}
	return nil
}

// providerSkip explains why a credential gate rejected a provider. It is
// logged only when the user configured the provider.
type providerSkip struct {
	msg   string
	attrs []any
}

// credentialGate decides whether a catalog provider has the credentials
// it needs, completing prepared with any provider-specific settings. It
// returns nil to keep the provider, or why it is skipped.
type credentialGate func(s *providerSetup, p catwalk.Provider, config ProviderConfig, prepared *ProviderConfig) *providerSkip

// credentialGates holds the providers whose credentials are not a plain
// API key. Every other provider goes through apiKeyCredentials.
var credentialGates = map[catwalk.InferenceProvider]credentialGate{
	catwalk.InferenceProviderVertexAI:      vertexCredentials,
	catwalk.InferenceProviderAzure:         azureCredentials,
	catwalk.InferenceProviderBedrock:       bedrockCredentials,
	catwalk.InferenceProviderBedrockEurope: bedrockCredentials,
	catwalk.InferenceProvider("hyper"):     hyperCredentials,
}

// vertexCredentials requires a Vertex AI project and location.
func vertexCredentials(s *providerSetup, _ catwalk.Provider, _ ProviderConfig, prepared *ProviderConfig) *providerSkip {
	project := s.env.Get("VERTEXAI_PROJECT")
	location := s.env.Get("VERTEXAI_LOCATION")
	if project == "" || location == "" {
		return &providerSkip{msg: "Skipping Vertex AI provider due to missing credentials"}
	}
	prepared.ExtraParams["project"] = project
	prepared.ExtraParams["location"] = location
	return nil
}

// azureCredentials requires the API endpoint to resolve.
func azureCredentials(s *providerSetup, p catwalk.Provider, _ ProviderConfig, prepared *ProviderConfig) *providerSkip {
	endpoint, err := s.resolver.ResolveValue(p.APIEndpoint)
	if err != nil || endpoint == "" {
		return &providerSkip{
			msg:   "Skipping Azure provider due to missing API endpoint",
			attrs: []any{"provider", p.ID, "error", err},
		}
	}
	prepared.BaseURL = endpoint
	prepared.ExtraParams["apiVersion"] = s.env.Get("AZURE_OPENAI_API_VERSION")
	return nil
}

// bedrockCredentials requires an API key or AWS credentials.
func bedrockCredentials(s *providerSetup, p catwalk.Provider, _ ProviderConfig, _ *ProviderConfig) *providerSkip {
	if p.APIKey == "" && !hasAWSCredentials(s.env) {
		return &providerSkip{msg: "Skipping Bedrock provider due to missing AWS credentials"}
	}
	return nil
}

// hyperCredentials prefers HYPER_API_KEY over the configured key.
func hyperCredentials(s *providerSetup, p catwalk.Provider, _ ProviderConfig, prepared *ProviderConfig) *providerSkip {
	if apiKey := s.env.Get("HYPER_API_KEY"); apiKey != "" {
		prepared.APIKey = apiKey
		prepared.APIKeyTemplate = apiKey
		return nil
	}
	if v, err := s.resolver.ResolveValue(p.APIKey); v == "" || err != nil {
		return &providerSkip{
			msg:   "Skipping Hyper provider due to missing API key",
			attrs: []any{"provider", p.ID},
		}
	}
	return nil
}

// apiKeyCredentials requires the API key to resolve, unless the provider
// is signed in through OAuth.
func apiKeyCredentials(s *providerSetup, p catwalk.Provider, config ProviderConfig, _ *ProviderConfig) *providerSkip {
	// An OAuth login is a credential too: providers signed in
	// through OAuth (e.g. OpenAI with a ChatGPT account) are
	// configured even when no API key is present.
	if config.OAuthToken != nil {
		return nil
	}
	if v, err := s.resolver.ResolveValue(p.APIKey); v == "" || err != nil {
		return &providerSkip{
			msg:   "Skipping provider due to missing API key",
			attrs: []any{"provider", p.ID},
		}
	}
	return nil
}

// discoveryResult is the outcome of discovering one custom provider's
// models.
type discoveryResult struct {
	models []catwalk.Model
	err    error
}

// discoverCustomModels discovers models concurrently for the custom
// providers that need it. A provider needs discovery when discover_models
// is explicitly true, or when the models list is empty (auto-trigger,
// unless opted out). The whole round is bounded to a few seconds.
func (s *providerSetup) discoverCustomModels(ctx context.Context) map[string]discoveryResult {
	results := make(map[string]discoveryResult)
	var mu sync.Mutex
	var wg sync.WaitGroup

	discoverCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	for id, pc := range s.cfg.Providers.Seq2() {
		if s.known[id] || !needsModelDiscovery(pc) {
			continue
		}
		cfg := discover.Config{
			ID:             cmp.Or(pc.ID, id),
			BaseURL:        pc.BaseURL,
			APIKey:         pc.APIKey,
			ExtraHeaders:   pc.ExtraHeaders,
			ExistingModels: pc.Models,
			KnownProviders: s.catalog,
		}
		providerType := cmp.Or(pc.Type, catwalk.TypeOpenAICompat)
		wg.Go(func() {
			models, err := discover.DiscoverModels(discoverCtx, cfg, s.resolver)
			if err == nil && len(models) > 0 {
				if enricher := discover.GetEnricher(string(providerType)); enricher != nil {
					models, _ = enricher.EnrichModels(discoverCtx, cfg, s.resolver, models)
				}
			}
			mu.Lock()
			results[id] = discoveryResult{models: models, err: err}
			mu.Unlock()
		})
	}
	wg.Wait()
	return results
}

// needsModelDiscovery reports whether a custom provider's models should be
// discovered from its endpoint.
func needsModelDiscovery(pc ProviderConfig) bool {
	if pc.Disable || pc.BaseURL == "" {
		return false
	}
	wantsDiscovery := pc.AutoDiscoverModels != nil && *pc.AutoDiscoverModels
	autoTrigger := len(pc.Models) == 0 && (pc.AutoDiscoverModels == nil || *pc.AutoDiscoverModels)
	return wantsDiscovery || autoTrigger
}

// validateCustomProviders normalizes every custom provider and drops the
// ones that cannot be used.
func (s *providerSetup) validateCustomProviders(discovered map[string]discoveryResult) error {
	for id, providerConfig := range s.cfg.Providers.Seq2() {
		if s.known[id] {
			continue
		}
		keep, err := s.validateCustomProvider(id, &providerConfig, discovered)
		if err != nil {
			return err
		}
		if !keep {
			s.cfg.Providers.Del(id)
			continue
		}
		s.cfg.Providers.Set(id, providerConfig)
	}
	return nil
}

// validateCustomProvider normalizes one custom provider in place and
// reports whether to keep it.
func (s *providerSetup) validateCustomProvider(id string, providerConfig *ProviderConfig, discovered map[string]discoveryResult) (bool, error) {
	// Make sure the provider ID is set.
	providerConfig.ID = id
	providerConfig.Name = cmp.Or(providerConfig.Name, id) // Use ID as name if not set
	// Default to OpenAI if not set.
	providerConfig.Type = cmp.Or(providerConfig.Type, catwalk.TypeOpenAICompat)
	if !isSupportedCustomProviderType(providerConfig.Type) {
		slog.Warn("Skipping custom provider due to unsupported provider type", "provider", id)
		return false, nil
	}

	if providerConfig.Disable {
		slog.Debug("Skipping custom provider due to disable flag", "provider", id)
		return false, nil
	}
	if IsCLIProviderType(providerConfig.Type) {
		// Agent CLIs bring their own login and endpoint.
		return true, nil
	}
	apiKey, err := s.resolver.ResolveValue(providerConfig.APIKey)
	if apiKey == "" || err != nil {
		slog.Warn("Provider is missing API key, this might be OK for local providers", "provider", id)
	}
	baseURL, err := s.resolver.ResolveValue(providerConfig.BaseURL)
	if baseURL == "" || err != nil {
		slog.Warn("Skipping custom provider due to missing API endpoint", "provider", id, "error", err)
		return false, nil
	}

	// Apply discovery results if available.
	if result, ok := discovered[id]; ok {
		if result.err != nil {
			slog.Warn("Model discovery failed", "provider", id, "error", result.err)
			if len(providerConfig.Models) == 0 {
				slog.Warn("Skipping provider with no models after failed discovery", "provider", id)
				return false, nil
			}
		} else if len(result.models) > 0 {
			providerConfig.Models = result.models
			slog.Info("Discovered models for provider", "provider", id, "count", len(result.models))
		}
	}

	if len(providerConfig.Models) == 0 {
		slog.Warn("Skipping custom provider because the provider has no models", "provider", id)
		return false, nil
	}

	// Custom-provider headers share the MCP error contract; see
	// configureKnownProvider.
	if err := resolveProviderHeaders(s.resolver, id, providerConfig.ExtraHeaders); err != nil {
		return false, err
	}
	return true, nil
}

// isSupportedCustomProviderType reports whether a custom provider's type
// is one Crush can talk to.
func isSupportedCustomProviderType(t catwalk.Type) bool {
	return slices.Contains(catwalk.KnownProviderTypes(), t) ||
		t == hyper.Name ||
		IsCLIProviderType(t) ||
		discover.IsKnownCustomProvider(string(t))
}

// applyEnv sets top-level env vars from the config. Keys are sorted for
// deterministic ordering so that vars referencing other vars via the
// value resolver produce consistent results.
func (c *Config) applyEnv(resolver VariableResolver) {
	keys := make([]string, 0, len(c.Env))
	for k := range c.Env {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		resolved, err := resolver.ResolveValue(c.Env[k])
		if err != nil {
			slog.Warn("Skipping env var due to resolution failure.", "key", k, "value", c.Env[k], "error", err)
			continue
		}
		os.Setenv(k, resolved)
	}
}

// NormalizeOptions allocates Options and Options.TUI and fills in the option
// defaults the UI relies on, so readers can dereference them without guarding.
// Configs loaded from disk get this via setDefaults; configs arriving over the
// wire from a Crush server need the same treatment before the UI reads them.
//
// DiffMode is deliberately left alone: the permissions dialog reads its zero
// value as "choose split or unified from the terminal width".
func (c *Config) NormalizeOptions() {
	if c.Options == nil {
		c.Options = &Options{}
	}
	if c.Options.TUI == nil {
		c.Options.TUI = &TUIOptions{}
	}
	if c.Options.TUI.Scrollbar == "" {
		c.Options.TUI.Scrollbar = ScrollbarDefault
	}
	if c.Options.TUI.ExitBanner == "" {
		c.Options.TUI.ExitBanner = ExitBannerDefault
	}
}

func (c *Config) setDefaults(workingDir, dataDir string) {
	c.NormalizeOptions()
	if len(c.Options.GlobalContextPaths) == 0 {
		crushConfigDir := filepath.Dir(GlobalConfig())
		c.Options.GlobalContextPaths = []string{
			filepath.Join(crushConfigDir, "CRUSH.md"),
			filepath.Join(filepath.Dir(crushConfigDir), "AGENTS.md"),
		}
	}
	slices.Sort(c.Options.GlobalContextPaths)
	c.Options.GlobalContextPaths = slices.Compact(c.Options.GlobalContextPaths)

	if dataDir != "" {
		c.Options.DataDirectory = dataDir
	} else if c.Options.DataDirectory == "" {
		if path, ok := fsext.LookupClosestBounded(workingDir, projectBoundary(workingDir), defaultDataDirectory); ok {
			c.Options.DataDirectory = path
		} else {
			c.Options.DataDirectory = filepath.Join(workingDir, defaultDataDirectory)
		}
	}
	c.Options.DataDirectory = filepath.Clean(filepathext.SmartJoin(workingDir, c.Options.DataDirectory))
	if c.Providers == nil {
		c.Providers = csync.NewMap[string, ProviderConfig]()
	}
	if c.Models == nil {
		c.Models = make(map[SelectedModelType]SelectedModel)
	}
	if c.RecentModels == nil {
		c.RecentModels = make(map[SelectedModelType][]SelectedModel)
	}
	if c.MCP == nil {
		c.MCP = make(map[string]MCPConfig)
	}
	// Drop orphaned OAuth token entries left behind when a user removes
	// an MCP from crush.json. See MCPConfig.isOrphanedToken.
	for name, m := range c.MCP {
		if m.isOrphanedToken() {
			delete(c.MCP, name)
		}
	}
	if c.LSP == nil {
		c.LSP = make(map[string]LSPConfig)
	}

	// Apply defaults to LSP configurations
	c.applyLSPDefaults()

	// Add the default context paths if they are not already present
	c.Options.ContextPaths = append(slices.Clone(defaultContextPaths), c.Options.ContextPaths...)

	slices.Sort(c.Options.ContextPaths)
	c.Options.ContextPaths = slices.Compact(c.Options.ContextPaths)

	if c.Options.DisableInstructionFiles {
		c.Options.SkillsPaths = []string{filepath.Join(home.Dir(), ".agents", "skills")}
	} else {
		// Add the default skills directories if not already present.
		for _, dir := range GlobalSkillsDirs() {
			if !slices.Contains(c.Options.SkillsPaths, dir) {
				c.Options.SkillsPaths = append(c.Options.SkillsPaths, dir)
			}
		}
		// Project specific skills dirs.
		c.Options.SkillsPaths = append(c.Options.SkillsPaths, ProjectSkillsDir(workingDir)...)
	}

	if str, ok := os.LookupEnv("CRUSH_DISABLE_PROVIDER_AUTO_UPDATE"); ok {
		c.Options.DisableProviderAutoUpdate, _ = strconv.ParseBool(str)
	}

	if str, ok := os.LookupEnv("CRUSH_DISABLE_DEFAULT_PROVIDERS"); ok {
		c.Options.DisableDefaultProviders, _ = strconv.ParseBool(str)
	}

	if c.Options.Attribution == nil {
		c.Options.Attribution = &Attribution{
			TrailerStyle:  TrailerStyleAssistedBy,
			GeneratedWith: true,
		}
	} else if c.Options.Attribution.TrailerStyle == "" {
		// Migrate deprecated co_authored_by or apply default
		if c.Options.Attribution.CoAuthoredBy != nil {
			if *c.Options.Attribution.CoAuthoredBy {
				c.Options.Attribution.TrailerStyle = TrailerStyleCoAuthoredBy
			} else {
				c.Options.Attribution.TrailerStyle = TrailerStyleNone
			}
		} else {
			c.Options.Attribution.TrailerStyle = TrailerStyleAssistedBy
		}
	}

	c.Options.InitializeAs = cmp.Or(c.Options.InitializeAs, defaultInitializeAs)
}

// powernapDefaults caches the powernap default LSP server catalog. The
// catalog is static and immutable for the life of the process, but
// building it (NewManager + LoadDefaults) is expensive and was previously
// repeated on every config reload. We load it once and only ever read from
// it via GetServer, so a shared instance is safe.
var (
	powernapDefaultsOnce sync.Once
	powernapDefaults     *powernapConfig.Manager
)

func lspDefaultsManager() *powernapConfig.Manager {
	powernapDefaultsOnce.Do(func() {
		m := powernapConfig.NewManager()
		// LoadDefaults only fails on malformed embedded defaults, which
		// would be a build-time bug; treat the manager as usable either
		// way so a transient error never wedges config loading.
		_ = m.LoadDefaults()
		powernapDefaults = m
	})
	return powernapDefaults
}

// applyLSPDefaults applies default values from powernap to LSP configurations
func (c *Config) applyLSPDefaults() {
	// Reuse the process-wide default catalog; building it per reload was a
	// significant chunk of reload latency.
	configManager := lspDefaultsManager()

	// Apply defaults to each LSP configuration
	for name, cfg := range c.LSP {
		// Try to get defaults from powernap based on name or command name.
		base, ok := configManager.GetServer(name)
		if !ok {
			base, ok = configManager.GetServer(cfg.Command)
			if !ok {
				continue
			}
		}
		if cfg.Options == nil {
			cfg.Options = base.Settings
		}
		if cfg.InitOptions == nil {
			cfg.InitOptions = base.InitOptions
		}
		if len(cfg.FileTypes) == 0 {
			cfg.FileTypes = base.FileTypes
		}
		if len(cfg.RootMarkers) == 0 {
			cfg.RootMarkers = base.RootMarkers
		}
		cfg.Command = cmp.Or(cfg.Command, base.Command)
		if len(cfg.Args) == 0 {
			cfg.Args = base.Args
		}
		if len(cfg.Env) == 0 {
			cfg.Env = base.Environment
		}
		// Update the config in the map
		c.LSP[name] = cfg
	}
}

func (c *Config) defaultModelSelection(knownProviders []catwalk.Provider) (largeModel SelectedModel, smallModel SelectedModel, err error) {
	if len(knownProviders) == 0 && c.Providers.Len() == 0 {
		err = fmt.Errorf("no providers configured, please configure at least one provider")
		return largeModel, smallModel, err
	}

	// Agent CLIs come first: they need no API key and are this fork's point.
	if large, small, ok := c.defaultCLIModels(); ok {
		return large, small, nil
	}

	// Use the first provider enabled based on the known providers order
	// if no provider found that is known use the first provider configured
	for _, p := range knownProviders {
		providerConfig, ok := c.Providers.Get(string(p.ID))
		if !ok || providerConfig.Disable {
			continue
		}
		defaultLargeModel := c.GetModel(string(p.ID), p.DefaultLargeModelID)
		if defaultLargeModel == nil {
			slog.Warn("Default large model %s not found for provider %s", p.DefaultLargeModelID, p.ID)
			if len(providerConfig.Models) == 0 {
				return largeModel, smallModel, fmt.Errorf("default large model %s not found for provider %s", p.DefaultLargeModelID, p.ID)
			}
			defaultLargeModel = &providerConfig.Models[0]
		}
		largeModel = SelectedModel{
			Provider:        string(p.ID),
			Model:           defaultLargeModel.ID,
			MaxTokens:       defaultLargeModel.DefaultMaxTokens,
			ReasoningEffort: defaultLargeModel.DefaultReasoningEffort,
		}

		defaultSmallModel := c.GetModel(string(p.ID), p.DefaultSmallModelID)
		if defaultSmallModel == nil {
			slog.Warn("Default small model %s not found for provider %s", p.DefaultSmallModelID, p.ID)
			if len(providerConfig.Models) == 0 {
				return largeModel, smallModel, fmt.Errorf("default small model %s not found for provider %s", p.DefaultSmallModelID, p.ID)
			}
			defaultSmallModel = &providerConfig.Models[0]
		}
		smallModel = SelectedModel{
			Provider:        string(p.ID),
			Model:           defaultSmallModel.ID,
			MaxTokens:       defaultSmallModel.DefaultMaxTokens,
			ReasoningEffort: defaultSmallModel.DefaultReasoningEffort,
		}
		return largeModel, smallModel, err
	}

	enabledProviders := c.EnabledProviders()
	slices.SortFunc(enabledProviders, func(a, b ProviderConfig) int {
		return strings.Compare(a.ID, b.ID)
	})

	if len(enabledProviders) == 0 {
		err = fmt.Errorf("no providers configured, please configure at least one provider")
		return largeModel, smallModel, err
	}

	providerConfig := enabledProviders[0]
	if len(providerConfig.Models) == 0 {
		err = fmt.Errorf("provider %s has no models configured", providerConfig.ID)
		return largeModel, smallModel, err
	}
	defaultLargeModel := c.GetModel(providerConfig.ID, providerConfig.Models[0].ID)
	largeModel = SelectedModel{
		Provider:  providerConfig.ID,
		Model:     defaultLargeModel.ID,
		MaxTokens: defaultLargeModel.DefaultMaxTokens,
	}
	defaultSmallModel := c.GetModel(providerConfig.ID, providerConfig.Models[0].ID)
	smallModel = SelectedModel{
		Provider:  providerConfig.ID,
		Model:     defaultSmallModel.ID,
		MaxTokens: defaultSmallModel.DefaultMaxTokens,
	}
	return largeModel, smallModel, err
}

// resolvedModels holds the result of resolving user-configured model
// selections against the provider catalog.
type resolvedModels struct {
	Large         SelectedModel
	Small         SelectedModel
	LargeFallback bool // true if Large was corrected to a default
	SmallFallback bool // true if Small was corrected to a default
}

// resolveSelectedModels validates the user's configured model selections
// against the provider catalog, falling back to defaults when a model ID is
// invalid. It is pure resolution logic: it does not mutate the store or
// touch disk. The caller assigns the results to c.Models and persists any
// fallback corrections as appropriate.
func resolveSelectedModels(cfg *Config, knownProviders []catwalk.Provider) (resolvedModels, error) {
	var result resolvedModels
	defaultLarge, defaultSmall, err := cfg.defaultModelSelection(knownProviders)
	if err != nil {
		return result, fmt.Errorf("failed to select default models: %w", err)
	}
	large, small := defaultLarge, defaultSmall

	largeModelSelected, largeModelConfigured := cfg.Models[SelectedModelTypeLarge]
	if largeModelConfigured {
		if largeModelSelected.Model != "" {
			large.Model = largeModelSelected.Model
		}
		if largeModelSelected.Provider != "" {
			large.Provider = largeModelSelected.Provider
		}
		model := cfg.GetModel(large.Provider, large.Model)
		if model == nil {
			large = defaultLarge
			result.LargeFallback = true
		} else {
			large.ServiceTier = largeModelSelected.ServiceTier
			large.Ultracode = largeModelSelected.Ultracode
			if largeModelSelected.MaxTokens > 0 {
				large.MaxTokens = largeModelSelected.MaxTokens
			} else {
				large.MaxTokens = model.DefaultMaxTokens
			}
			if largeModelSelected.ReasoningEffort != "" {
				large.ReasoningEffort = largeModelSelected.ReasoningEffort
			} else {
				large.ReasoningEffort = model.DefaultReasoningEffort
			}
			large.Think = largeModelSelected.Think
			if largeModelSelected.Temperature != nil {
				large.Temperature = largeModelSelected.Temperature
			}
			if largeModelSelected.TopP != nil {
				large.TopP = largeModelSelected.TopP
			}
			if largeModelSelected.TopK != nil {
				large.TopK = largeModelSelected.TopK
			}
			if largeModelSelected.FrequencyPenalty != nil {
				large.FrequencyPenalty = largeModelSelected.FrequencyPenalty
			}
			if largeModelSelected.PresencePenalty != nil {
				large.PresencePenalty = largeModelSelected.PresencePenalty
			}
			if largeModelSelected.ProviderOptions != nil {
				large.ProviderOptions = maps.Clone(largeModelSelected.ProviderOptions)
			}
		}
	}
	smallModelSelected, smallModelConfigured := cfg.Models[SelectedModelTypeSmall]
	if smallModelConfigured {
		if smallModelSelected.Model != "" {
			small.Model = smallModelSelected.Model
		}
		if smallModelSelected.Provider != "" {
			small.Provider = smallModelSelected.Provider
		}

		model := cfg.GetModel(small.Provider, small.Model)
		if model == nil {
			small = defaultSmall
			result.SmallFallback = true
		} else {
			small.ServiceTier = smallModelSelected.ServiceTier
			small.Ultracode = smallModelSelected.Ultracode
			if smallModelSelected.MaxTokens > 0 {
				small.MaxTokens = smallModelSelected.MaxTokens
			} else {
				small.MaxTokens = model.DefaultMaxTokens
			}
			if smallModelSelected.ReasoningEffort != "" {
				small.ReasoningEffort = smallModelSelected.ReasoningEffort
			} else {
				small.ReasoningEffort = model.DefaultReasoningEffort
			}
			if smallModelSelected.Temperature != nil {
				small.Temperature = smallModelSelected.Temperature
			}
			if smallModelSelected.TopP != nil {
				small.TopP = smallModelSelected.TopP
			}
			if smallModelSelected.TopK != nil {
				small.TopK = smallModelSelected.TopK
			}
			if smallModelSelected.FrequencyPenalty != nil {
				small.FrequencyPenalty = smallModelSelected.FrequencyPenalty
			}
			if smallModelSelected.PresencePenalty != nil {
				small.PresencePenalty = smallModelSelected.PresencePenalty
			}
			if smallModelSelected.ProviderOptions != nil {
				small.ProviderOptions = maps.Clone(smallModelSelected.ProviderOptions)
			}
			small.Think = smallModelSelected.Think
		}
	}

	// When small isn't explicitly configured and the provider isn't a
	// known built-in, use the large model as the small model. This
	// prevents two different models from being requested concurrently
	// for local/openai-compat providers.
	if !smallModelConfigured {
		isKnownProvider := false
		for _, kp := range knownProviders {
			if string(kp.ID) == small.Provider {
				isKnownProvider = true
				break
			}
		}
		if cliSmall, ok := cfg.CLISmallModel(large.Provider); ok {
			small = cliSmall
		} else if !isKnownProvider {
			slog.Warn("Using large model as small model for unknown provider", "provider", large.Provider, "model", large.Model)
			small = large
		}
	}

	result.Large = large
	result.Small = small
	return result, nil
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

func hasAWSCredentials(env env.Env) bool {
	if env.Get("AWS_BEARER_TOKEN_BEDROCK") != "" {
		return true
	}

	if env.Get("AWS_ACCESS_KEY_ID") != "" && env.Get("AWS_SECRET_ACCESS_KEY") != "" {
		return true
	}

	if env.Get("AWS_PROFILE") != "" || env.Get("AWS_DEFAULT_PROFILE") != "" {
		return true
	}

	if env.Get("AWS_REGION") != "" || env.Get("AWS_DEFAULT_REGION") != "" {
		return true
	}

	if env.Get("AWS_CONTAINER_CREDENTIALS_RELATIVE_URI") != "" ||
		env.Get("AWS_CONTAINER_CREDENTIALS_FULL_URI") != "" {
		return true
	}

	// File-based credential discovery requires filesystem stats, so do it
	// last and skip it under test. Checking testing.Testing() before the
	// os.Stat call (rather than after, in the && tail) ensures the syscall
	// is never issued during tests, where it otherwise ran unconditionally
	// and only had its result discarded.
	if testing.Testing() {
		return false
	}
	if _, err := os.Stat(filepath.Join(home.Dir(), ".aws/credentials")); err == nil {
		return true
	}
	if _, err := os.Stat(filepath.Join(home.Dir(), ".aws/login")); err == nil {
		return true
	}

	return false
}

// migrateDisableNotifications migrates the deprecated disable_notifications
// and notification_style fields to the unified notifications field. It checks
// both the user config (~/.config) and data config (~/.local) files. If
// disable_notifications is true, it sets notifications to "disabled" in the
// data file. If notification_style is set, it moves the value to notifications.
// Regardless of value, it removes the deprecated fields from any file that
// contains them.
func migrateDisableNotifications() {
	globalConfig := GlobalConfig()
	dataConfig := GlobalConfigData()

	var wasDisabled bool
	var styleValue string
	filesToClean := []string{}

	for _, path := range []string{globalConfig, dataConfig} {
		data, err := readFile(path)
		if err != nil {
			continue
		}
		needsClean := false
		if gjson.Get(string(data), "options.disable_notifications").Exists() {
			needsClean = true
			if gjson.Get(string(data), "options.disable_notifications").Bool() {
				wasDisabled = true
			}
		}
		if v := gjson.Get(string(data), "options.notification_style"); v.Exists() {
			needsClean = true
			if styleValue == "" {
				styleValue = v.String()
			}
		}
		if needsClean {
			filesToClean = append(filesToClean, path)
		}
	}

	if len(filesToClean) == 0 {
		return
	}

	// Determine the value to persist: notification_style takes precedence,
	// then disable_notifications: true maps to "disabled".
	migratedValue := styleValue
	if migratedValue == "" && wasDisabled {
		migratedValue = "disabled"
	}

	if migratedValue != "" {
		data, err := readFile(dataConfig)
		if err == nil {
			if !gjson.Get(string(data), "options.notifications").Exists() {
				updated, err := sjson.Set(string(data), "options.notifications", migratedValue)
				if err == nil {
					if err := atomicWriteFile(dataConfig, []byte(updated), 0o600); err != nil {
						slog.Warn("Failed to migrate to notifications field", "error", err)
					} else {
						slog.Info("Migrated notification settings to notifications field", "value", migratedValue)
					}
				}
			}
		}
	}

	// Remove deprecated fields from all files that contain them.
	for _, path := range filesToClean {
		data, err := readFile(path)
		if err != nil {
			continue
		}
		updated := string(data)
		updated, _ = sjson.Delete(updated, "options.disable_notifications")
		updated, _ = sjson.Delete(updated, "options.notification_style")
		if updated == string(data) {
			continue
		}
		if err := atomicWriteFile(path, []byte(updated), 0o600); err != nil {
			slog.Warn("Failed to write migrated config", "path", path, "error", err)
		}
	}
}

// GlobalConfig returns the global configuration file path for the application.
func GlobalConfig() string {
	if crushGlobal := os.Getenv("CRUSH_GLOBAL_CONFIG"); crushGlobal != "" {
		return filepath.Join(crushGlobal, fmt.Sprintf("%s.json", appName))
	}
	return filepath.Join(home.Config(), appName, fmt.Sprintf("%s.json", appName))
}

// shellConfigSibling returns the crushrc path that sits alongside a given
// crush.json path (same directory). Used so global config locations pick up a
// shell config, not just JSON.
func shellConfigSibling(jsonPath string) string {
	return filepath.Join(filepath.Dir(jsonPath), appName+"rc")
}

// isShellConfig reports whether a config path is a shell config (crushrc or
// the hidden .crushrc), as opposed to a JSON config.
func isShellConfig(path string) bool {
	base := filepath.Base(path)
	return base == appName+"rc" || base == "."+appName+"rc"
}

// GlobalCacheDir returns the path to the global cache directory for the
// application.
func GlobalCacheDir() string {
	if crushCache := os.Getenv("CRUSH_CACHE_DIR"); crushCache != "" {
		return crushCache
	}
	if xdgCacheHome := os.Getenv("XDG_CACHE_HOME"); xdgCacheHome != "" {
		return filepath.Join(xdgCacheHome, appName)
	}
	if runtime.GOOS == "windows" {
		localAppData := cmp.Or(
			os.Getenv("LOCALAPPDATA"),
			filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Local"),
		)
		return filepath.Join(localAppData, appName, "cache")
	}
	return filepath.Join(home.Dir(), ".cache", appName)
}

// ProjectConfigs returns list of current project configs paths.
func ProjectConfigs(cwd string) []string {
	return lookupConfigs(cwd)
}

// GlobalConfigData returns the path to the main data directory for the application.
// this config is used when the app overrides configurations instead of updating the global config.
func GlobalConfigData() string {
	if crushData := os.Getenv("CRUSH_GLOBAL_DATA"); crushData != "" {
		return filepath.Join(crushData, fmt.Sprintf("%s.json", appName))
	}
	if xdgDataHome := os.Getenv("XDG_DATA_HOME"); xdgDataHome != "" {
		return filepath.Join(xdgDataHome, appName, fmt.Sprintf("%s.json", appName))
	}

	// return the path to the main data directory
	// for windows, it should be in `%LOCALAPPDATA%/crush/`
	// for linux and macOS, it should be in `$HOME/.local/share/crush/`
	if runtime.GOOS == "windows" {
		localAppData := cmp.Or(
			os.Getenv("LOCALAPPDATA"),
			filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Local"),
		)
		return filepath.Join(localAppData, appName, fmt.Sprintf("%s.json", appName))
	}

	return filepath.Join(home.Dir(), ".local", "share", appName, fmt.Sprintf("%s.json", appName))
}

// GlobalWorkspaceDir returns the path to the global server workspace
// directory. This directory acts as a meta-workspace for the server
// process, giving it a real workingDir so that config loading, scoped
// writes, and provider resolution behave identically to project
// workspaces.
func GlobalWorkspaceDir() string {
	return filepath.Dir(GlobalConfigData())
}

func assignIfNil[T any](ptr **T, val T) {
	if *ptr == nil {
		*ptr = &val
	}
}

func isInsideWorktree() bool {
	bts, err := exec.CommandContext(
		context.Background(),
		"git", "rev-parse",
		"--is-inside-work-tree",
	).CombinedOutput()
	return err == nil && strings.TrimSpace(string(bts)) == "true"
}

// worktreeRoot returns the absolute path of the git working tree root for
// dir, or the empty string if dir is not inside a working tree (bare
// repositories, missing git binary, plain directories, or any other
// failure mode). Linked worktrees and submodules each report their own
// top-level, which is what callers want when bounding lookups.
// worktreeRootCache memoizes the git worktree root per directory. The root
// is stable for the life of the process, so we avoid re-shelling out to
// "git rev-parse" on every config reload. Keyed by the requested dir; the
// value is the resolved root ("" when dir is not in a git worktree).
var worktreeRootCache sync.Map // map[string]string

func worktreeRoot(dir string) string {
	if cached, ok := worktreeRootCache.Load(dir); ok {
		return cached.(string)
	}
	root := computeWorktreeRoot(dir)
	worktreeRootCache.Store(dir, root)
	return root
}

func computeWorktreeRoot(dir string) string {
	cmd := exec.CommandContext(
		context.Background(),
		"git", "rev-parse", "--show-toplevel",
	)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	root := strings.TrimSpace(string(out))
	if root == "" {
		return ""
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return ""
	}
	return abs
}

// projectBoundary returns the directory at which an upward configuration
// search rooted at dir should stop. It is the git working tree root when
// one can be detected, otherwise dir itself. Returning dir as a
// fallback keeps Crush from silently adopting state files placed above
// the current project.
func projectBoundary(dir string) string {
	if root := worktreeRoot(dir); root != "" {
		return root
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return dir
	}
	return abs
}

// GlobalSkillsDirs returns the default directories for Agent Skills.
// Skills in these directories are auto-discovered and their files can be read
// without permission prompts.
func GlobalSkillsDirs() []string {
	if crushSkills := os.Getenv("CRUSH_SKILLS_DIR"); crushSkills != "" {
		return []string{crushSkills}
	}

	paths := []string{
		filepath.Join(home.Config(), appName, "skills"),
		filepath.Join(home.Config(), "agents", "skills"),
		// Per the Agent Skills spec, scan ~/.agents/skills
		filepath.Join(home.Dir(), ".agents", "skills"),
		filepath.Join(home.Dir(), ".claude", "skills"),
	}

	// On Windows, also load from app data on top of `$HOME/.config/crush`.
	// This is here mostly for backwards compatibility.
	if runtime.GOOS == "windows" {
		appData := cmp.Or(
			os.Getenv("LOCALAPPDATA"),
			filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Local"),
		)
		paths = append(
			paths,
			filepath.Join(appData, appName, "skills"),
			filepath.Join(appData, "agents", "skills"),
		)
	}

	return paths
}

// projectSkillSubdirs lists the conventional subdirectories where
// project-level skills are discovered. Shared across working-dir and
// git-root lookups to prevent drift when a new convention is added.
var projectSkillSubdirs = []string{
	".agents/skills",
	".crush/skills",
	".claude/skills",
	".cursor/skills",
}

// ProjectSkillsDir returns the default project directories for which Crush
// will look for skills. In addition to the working directory, it also
// checks the git working tree root so that monorepo-level skills are
// discovered when the user is inside a subdirectory.
// Working-directory paths come first so local skills take precedence
// over monorepo-level ones.
func ProjectSkillsDir(workingDir string) []string {
	dirs := make([]string, 0, len(projectSkillSubdirs)*2)
	for _, sub := range projectSkillSubdirs {
		dirs = append(dirs, filepath.Join(workingDir, sub))
	}

	// When the working directory is inside a git repository, also look at
	// the repository root so monorepo-level .agents/skills are found.
	if root := worktreeRoot(workingDir); root != "" && root != workingDir {
		for _, sub := range projectSkillSubdirs {
			dirs = append(dirs, filepath.Join(root, sub))
		}
	}

	return dirs
}

func isAppleTerminal() bool { return os.Getenv("TERM_PROGRAM") == "Apple_Terminal" }

// normalizeHookEvent maps user-provided event names to their canonical
// form. Matching is case-insensitive and accepts snake_case variants
// (e.g. "pre_tool_use" → "PreToolUse").
func normalizeHookEvent(name string) string {
	switch strings.ToLower(strings.ReplaceAll(name, "_", "")) {
	case "pretooluse":
		return "PreToolUse"
	default:
		return name
	}
}

// ValidateHooks normalizes event names and checks that every configured
// hook has a command and a syntactically valid matcher regex. Matcher
// compilation used for matching is owned by hooks.Runner; this function
// only validates up front so the user sees config errors at load time
// rather than on the first tool call.
func (c *Config) ValidateHooks() error {
	// Normalize event name keys.
	for event, eventHooks := range c.Hooks {
		canonical := normalizeHookEvent(event)
		if canonical != event {
			c.Hooks[canonical] = append(c.Hooks[canonical], eventHooks...)
			delete(c.Hooks, event)
		}
	}

	for event, eventHooks := range c.Hooks {
		for i, h := range eventHooks {
			if h.Command == "" {
				return fmt.Errorf("hook %s[%d]: command is required", event, i)
			}
			if h.Matcher == "" {
				continue
			}
			if _, err := regexp.Compile(h.Matcher); err != nil {
				return fmt.Errorf("hook %s[%d]: invalid matcher regex %q: %w", event, i, h.Matcher, err)
			}
		}
	}
	return nil
}

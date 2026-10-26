package config

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/agent/hyper"
	"github.com/charmbracelet/crush/internal/discover"
	"github.com/charmbracelet/crush/internal/env"
	"github.com/charmbracelet/crush/internal/home"
)

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

	// Before the custom-provider check: a key saved from Settings is the
	// whole entry, and this fills in the rest.
	if listCodexAPI {
		c.addCodexAPIProvider()
	}
	if listTinfoil {
		c.addTinfoilProviders(ctx)
	}

	discovered := s.discoverCustomModels(ctx)
	s.applyDynamicModels(discovered)

	c.addCLIProviders(env.Get("PATH"))

	if err := s.validateCustomProviders(discovered); err != nil {
		return s.pending, err
	}

	if c.Providers.Len() == 0 && c.Options.DisableDefaultProviders {
		return s.pending, fmt.Errorf("default providers are disabled and there are no custom providers are configured")
	}

	s.dropVendorWords()

	return s.pending, nil
}

// dropVendorWords drops the vendor word OpenRouter puts before its model
// names, for the models saved in the config and the catalog's own alike, so
// the apps show "DeepSeek V4.1 Flash 1017" rather than "DeepSeek: DeepSeek
// V4.1 Flash 1017" (owner, 2026-10-22). Other providers keep their names.
func (s *providerSetup) dropVendorWords() {
	for id, providerConfig := range s.cfg.Providers.Seq2() {
		if providerConfig.Type != catwalk.TypeOpenRouter {
			continue
		}
		changed := false
		models := slices.Clone(providerConfig.Models)
		for i, model := range models {
			if plain := discover.OpenRouterModelName(model.Name); plain != model.Name {
				models[i].Name = plain
				changed = true
			}
		}
		if !changed {
			continue
		}
		providerConfig.Models = models
		s.cfg.Providers.Set(id, providerConfig)
	}
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
	if planProvider(p.ID) {
		prepared.FlatRate = true
	}
	if prepared.ExtraParams == nil {
		prepared.ExtraParams = make(map[string]string)
	}
	return prepared
}

// planProvider lists the catalog providers sold as a paid plan, not billed
// per use: Kimi Coding (owner, 2026-10-24). They come out flat-rate, so no
// cost is counted and the apps don't list them as per-use APIs.
func planProvider(id catwalk.InferenceProvider) bool {
	return id == catwalk.InferenceKimiCoding
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
		dynamic := dynamicModelProvider(id)
		if s.known[id] && !dynamic {
			continue
		}
		if pc.Disable || pc.BaseURL == "" {
			continue
		}
		if dynamic {
			// The provider's own list is the model list, so it is read
			// whenever a key can pay for the request.
			if !pc.HasAPIKey(s.resolver) {
				continue
			}
		} else if !needsModelDiscovery(pc) {
			continue
		}
		providerType := cmp.Or(pc.Type, catwalk.TypeOpenAICompat)
		cfg := discover.Config{
			ID:             cmp.Or(pc.ID, id),
			Type:           providerType,
			BaseURL:        pc.BaseURL,
			APIKey:         pc.APIKey,
			ExtraHeaders:   pc.ExtraHeaders,
			ExistingModels: pc.Models,
			KnownProviders: s.catalog,
		}
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

// dynamicModelProvider lists the catalog providers whose model list always
// comes from their own API, even though the catalog carries a list for
// them: Kimi changes its plans' models without a Crush release (owner,
// 2026-10-24). Their catalog models supply the details of the ids that
// come back, and nothing is listed that the API does not return.
func dynamicModelProvider(id string) bool {
	return catwalk.InferenceProvider(id) == catwalk.InferenceKimiCoding
}

// applyDynamicModels writes a dynamic provider's freshly read model list
// over the catalog's, so the apps offer exactly what the API lists.
func (s *providerSetup) applyDynamicModels(discovered map[string]discoveryResult) {
	for id, result := range discovered {
		if !dynamicModelProvider(id) || result.err != nil || len(result.models) == 0 {
			continue
		}
		providerConfig, ok := s.cfg.Providers.Get(id)
		if !ok {
			continue
		}
		providerConfig.Models = result.models
		s.cfg.Providers.Set(id, providerConfig)
	}
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

	// OpenRouter's models are saved from its catalog in the app, so it stays
	// with none saved yet: the app shows its "+" there.
	if len(providerConfig.Models) == 0 && providerConfig.Type != catwalk.TypeOpenRouter {
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

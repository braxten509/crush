package config

import (
	"context"
	"fmt"
	"log/slog"

	"charm.land/catwalk/pkg/catwalk"
	hyperp "github.com/charmbracelet/crush/internal/agent/hyper"
	"github.com/charmbracelet/crush/internal/oauth"
	"github.com/charmbracelet/crush/internal/oauth/copilot"
	"github.com/charmbracelet/crush/internal/oauth/grok"
	"github.com/charmbracelet/crush/internal/oauth/openai"
)

// RefetchHyperProvider re-fetches the Hyper provider catalog from the
// remote API and updates the in-memory known providers list and config.
// This is called after OAuth authentication completes so the latest
// models are available without restarting.
func (s *ConfigStore) RefetchHyperProvider(ctx context.Context) error {
	// Build a fresh client that reads the API key from the live config,
	// not the stale snapshot captured at startup. The syncer's original
	// client closes over the startup config and would send an expired
	// token after OAuth re-authentication.
	freshClient := realHyperClient{
		baseURL:    hyperp.BaseURL(),
		resolveKey: func() string { return ResolveHyperAPIKey(s.Config()) },
	}
	hyperSyncer.SetClient(freshClient)

	hyperProvider, err := hyperSyncer.Refetch(ctx)
	if err != nil {
		return fmt.Errorf("failed to refetch Hyper provider: %w", err)
	}
	if hyperProvider.ID == "" {
		return nil
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	// Replace or insert the Hyper entry in knownProviders.
	found := false
	for i, p := range s.knownProviders {
		if string(p.ID) == string(hyperProvider.ID) {
			s.knownProviders[i] = hyperProvider
			found = true
			break
		}
	}
	if !found {
		s.knownProviders = append([]catwalk.Provider{hyperProvider}, s.knownProviders...)
	}

	// Update the Hyper provider config with the refreshed model list
	// and endpoint. Use cloneForWrite so readers always see a consistent
	// snapshot (the store's contract forbids in-place config mutation).
	nc := s.config.cloneForWrite()
	if pc, ok := nc.Providers.Get(string(hyperProvider.ID)); ok {
		pc.Models = hyperProvider.Models
		if hyperProvider.APIEndpoint != "" {
			pc.BaseURL = hyperProvider.APIEndpoint
		}
		nc.Providers.Set(string(hyperProvider.ID), pc)
	}
	nc.SetupAgents()
	s.setConfig(nc)

	// Also update the memoized provider list so callers of
	// config.Providers() (e.g. the models dialog) see fresh data.
	UpdateProviderInList(hyperProvider)

	return nil
}

// SetProviderAPIKey sets the API key for a provider and persists it.
// The OpenAI provider holds exactly one credential: storing a ChatGPT
// token removes a previously entered API key, and storing an API key
// removes a previous ChatGPT login.
func (s *ConfigStore) SetProviderAPIKey(scope Scope, providerID string, apiKey any) error {
	var setKeyOrToken func(*ProviderConfig)
	isToken := false

	switch v := apiKey.(type) {
	case string:
		if err := s.persistAPIKey(scope, providerID, v); err != nil {
			return err
		}
		setKeyOrToken = func(pc *ProviderConfig) { applyAPIKey(pc, providerID, v) }
	case *oauth.Token:
		isToken = true
		if err := s.persistOAuthToken(scope, providerID, v); err != nil {
			return err
		}
		setKeyOrToken = func(pc *ProviderConfig) { applyOAuthLogin(pc, providerID, v) }
	}

	if err := s.setProviderCredential(providerID, setKeyOrToken); err != nil {
		return err
	}
	s.refreshCatalogsAfterAuth(scope, providerID, isToken)
	return nil
}

// oauthCatalogs lists the providers whose OAuth login grants its own model
// catalog, keyed by provider ID. These providers hold either an OAuth
// login or an API key, never both: storing a key retires the login along
// with its catalog.
var oauthCatalogs = map[string]struct {
	// field is the provider config key the catalog is stored under.
	field string
	// clear drops the catalog from an in-memory provider config.
	clear func(*ProviderConfig)
}{
	string(catwalk.InferenceProviderOpenAI): {
		field: "chatgpt_models",
		clear: func(pc *ProviderConfig) { pc.ChatGPTModels = nil },
	},
	string(catwalk.InferenceProviderXAI): {
		field: "grok_models",
		clear: func(pc *ProviderConfig) { pc.GrokModels = nil },
	},
}

// persistAPIKey writes an API key to the config file, retiring an OAuth
// login it replaces.
func (s *ConfigStore) persistAPIKey(scope Scope, providerID, key string) error {
	if err := s.SetConfigField(scope, fmt.Sprintf("providers.%s.api_key", providerID), key); err != nil {
		return fmt.Errorf("failed to save api key to config file: %w", err)
	}
	catalog, ok := oauthCatalogs[providerID]
	if !ok {
		return nil
	}
	// Either OAuth or an API key, never both: the new key leaves nothing
	// usable on the login side behind.
	if err := s.RemoveConfigField(scope, fmt.Sprintf("providers.%s.oauth", providerID)); err != nil {
		return err
	}
	return s.RemoveConfigField(scope, fmt.Sprintf("providers.%s.%s", providerID, catalog.field))
}

// applyAPIKey sets an API key on an in-memory provider config.
func applyAPIKey(pc *ProviderConfig, providerID, key string) {
	pc.APIKey = key
	if catalog, ok := oauthCatalogs[providerID]; ok {
		// Either OAuth or an API key, never both: the key replaces a
		// previous login, whose refreshes would otherwise overwrite the
		// key again.
		pc.OAuthToken = nil
		catalog.clear(pc)
	}
}

// persistOAuthToken writes an OAuth token to the config file.
func (s *ConfigStore) persistOAuthToken(scope Scope, providerID string, token *oauth.Token) error {
	// Hold the refresh lock across the write so a peer's in-flight
	// token exchange cannot land on top of a credential the user just
	// obtained interactively — which would silently invalidate the
	// login they only just completed.
	if err := s.withRefreshLock(providerID, func() error {
		return s.SetConfigFields(scope, tokenFields(providerID, token))
	}); err != nil {
		return err
	}
	if providerID == string(catwalk.InferenceProviderOpenAI) {
		// Either OAuth or an API key, never both: the login retires
		// any key that came before it.
		return s.RemoveConfigField(scope, fmt.Sprintf("providers.%s.api_key", providerID))
	}
	return nil
}

// applyOAuthLogin sets an OAuth token on an in-memory provider config.
func applyOAuthLogin(pc *ProviderConfig, providerID string, token *oauth.Token) {
	pc.OAuthToken = token
	if providerID == string(catwalk.InferenceProviderOpenAI) {
		pc.APIKey = ""
		return
	}
	pc.APIKey = token.AccessToken
	if providerID == string(catwalk.InferenceProviderCopilot) {
		pc.SetupGitHubCopilot()
	}
}

// setProviderCredential applies a credential change to the provider's
// in-memory config, creating the config from the known provider catalog
// when the provider is not configured yet.
func (s *ConfigStore) setProviderCredential(providerID string, apply func(*ProviderConfig)) error {
	cfg := s.Config()
	providerConfig, exists := cfg.Providers.Get(providerID)
	if !exists {
		var foundProvider *catwalk.Provider
		for _, p := range s.knownProviders {
			if string(p.ID) == providerID {
				foundProvider = &p
				break
			}
		}
		if foundProvider == nil {
			return fmt.Errorf("provider with ID %s not found in known providers", providerID)
		}
		providerConfig = ProviderConfig{
			ID:           providerID,
			Name:         foundProvider.Name,
			BaseURL:      foundProvider.APIEndpoint,
			Type:         foundProvider.Type,
			Disable:      false,
			ExtraHeaders: make(map[string]string),
			ExtraParams:  make(map[string]string),
			Models:       foundProvider.Models,
		}
	}
	apply(&providerConfig)
	cfg.Providers.Set(providerID, providerConfig)
	return nil
}

// refreshCatalogsAfterAuth fetches the model catalogs a new credential
// unlocks, so the latest models are available without restarting.
func (s *ConfigStore) refreshCatalogsAfterAuth(scope Scope, providerID string, isToken bool) {
	// After authenticating with Hyper, re-fetch the provider catalog.
	if providerID == "hyper" {
		if refetchErr := s.RefetchHyperProvider(context.Background()); refetchErr != nil {
			slog.Warn("Failed to refetch Hyper provider after auth", "error", refetchErr)
		}
	}
	// After authenticating with a ChatGPT account, fetch the Codex model
	// catalog the subscription grants and persist it so the models
	// dialog can offer it beside the API-key catalog.
	if providerID == string(catwalk.InferenceProviderOpenAI) && isToken {
		s.refetchOpenAIModels(context.Background(), scope)
	}
	// Same for a Grok account: fetch the model catalog the Grok plan
	// grants and persist it.
	if providerID == string(catwalk.InferenceProviderXAI) && isToken {
		s.refetchGrokModels(context.Background(), scope)
	}
}

// fetchOpenAIModels fetches the ChatGPT model catalog from the Codex
// backend. A package variable so tests can stub the network call,
// matching how the catwalk and hyper syncers are swappable globals.
var fetchOpenAIModels = openai.Models

// refetchOpenAIModels stores the Codex model catalog the ChatGPT plan
// grants next to the provider's API-key models. Best effort: a failure
// leaves the existing catalog in place and the login still succeeds.
func (s *ConfigStore) refetchOpenAIModels(ctx context.Context, scope Scope) {
	const providerID = string(catwalk.InferenceProviderOpenAI)
	pc, ok := s.Config().Providers.Get(providerID)
	if !ok || pc.OAuthToken == nil {
		return
	}
	// The access token may have expired since login, so renew it before
	// asking for the catalog: the models endpoint rejects stale tokens
	// with a 401. A failed refresh falls through and lets the fetch run
	// on the old token, which keeps the existing catalog in place.
	if pc.OAuthToken.IsExpired() {
		if err := s.RefreshOAuthToken(ctx, scope, providerID); err != nil {
			slog.Warn("Failed to refresh the ChatGPT token before fetching the model catalog", "error", err)
		}
		if refreshed, ok := s.Config().Providers.Get(providerID); ok && refreshed.OAuthToken != nil {
			pc = refreshed
		}
	}
	models, err := fetchOpenAIModels(ctx, pc.OAuthToken)
	if err != nil {
		slog.Warn("Failed to fetch ChatGPT model catalog after auth", "error", err)
		return
	}
	if err := s.update(scope, func(c *Config) map[string]any {
		p, ok := c.Providers.Get(providerID)
		if !ok {
			return nil
		}
		p.ChatGPTModels = models
		c.Providers.Set(providerID, p)
		return map[string]any{
			"providers.openai.chatgpt_models": models,
		}
	}); err != nil {
		slog.Warn("Failed to persist ChatGPT model catalog", "error", err)
	}
}

// RefetchOpenAIChatGPTModels fills in the ChatGPT model catalog when the
// OpenAI provider is signed in but has none — because the fetch at login
// time failed, or the credentials predate the catalog. A no-op once the
// catalog exists, so callers can invoke it freely on model updates: an
// existing catalog is refreshed at startup instead, when Catwalk delivers
// a new one (see Load).
func (s *ConfigStore) RefetchOpenAIChatGPTModels(ctx context.Context) {
	cfg := s.Config()
	pc, ok := cfg.Providers.Get(string(catwalk.InferenceProviderOpenAI))
	if !ok || pc.OAuthToken == nil || len(pc.ChatGPTModels) > 0 {
		return
	}
	s.refetchOpenAIModels(ctx, ScopeGlobal)
}

// fetchGrokModels fetches the Grok model catalog from the xAI API. A
// package variable so tests can stub the network call, matching how the
// catwalk and hyper syncers are swappable globals.
var fetchGrokModels = grok.Models

// refetchGrokModels stores the model catalog the Grok plan grants next
// to the provider's API-key models. Best effort: a failure leaves the
// existing catalog in place and the login still succeeds.
func (s *ConfigStore) refetchGrokModels(ctx context.Context, scope Scope) {
	const providerID = string(catwalk.InferenceProviderXAI)
	pc, ok := s.Config().Providers.Get(providerID)
	if !ok || pc.OAuthToken == nil {
		return
	}
	// The access token may have expired since login, so renew it before
	// asking for the catalog: the models endpoint rejects stale tokens
	// with a 401. A failed refresh falls through and lets the fetch run
	// on the old token, which keeps the existing catalog in place.
	if pc.OAuthToken.IsExpired() {
		if err := s.RefreshOAuthToken(ctx, scope, providerID); err != nil {
			slog.Warn("Failed to refresh the Grok token before fetching the model catalog", "error", err)
		}
		if refreshed, ok := s.Config().Providers.Get(providerID); ok && refreshed.OAuthToken != nil {
			pc = refreshed
		}
	}
	models, err := fetchGrokModels(ctx, pc.OAuthToken)
	if err != nil {
		slog.Warn("Failed to fetch Grok model catalog after auth", "error", err)
		return
	}
	if err := s.update(scope, func(c *Config) map[string]any {
		p, ok := c.Providers.Get(providerID)
		if !ok {
			return nil
		}
		p.GrokModels = models
		c.Providers.Set(providerID, p)
		return map[string]any{
			"providers.xai.grok_models": models,
		}
	}); err != nil {
		slog.Warn("Failed to persist Grok model catalog", "error", err)
	}
}

// RefetchGrokModels fills in the Grok model catalog when the xAI
// provider is signed in but has none — because the fetch at login time
// failed, or the credentials predate the catalog. A no-op once the
// catalog exists, so callers can invoke it freely on model updates: an
// existing catalog is refreshed at startup instead, when Catwalk
// delivers a new one (see Load).
func (s *ConfigStore) RefetchGrokModels(ctx context.Context) {
	cfg := s.Config()
	pc, ok := cfg.Providers.Get(string(catwalk.InferenceProviderXAI))
	if !ok || pc.OAuthToken == nil || len(pc.GrokModels) > 0 {
		return
	}
	s.refetchGrokModels(ctx, ScopeGlobal)
}

// ImportCopilot attempts to import a GitHub Copilot token from disk.
func (s *ConfigStore) ImportCopilot() (*oauth.Token, bool) {
	if s.HasConfigField(ScopeGlobal, "providers.copilot.api_key") || s.HasConfigField(ScopeGlobal, "providers.copilot.oauth") {
		return nil, false
	}

	diskToken, hasDiskToken := copilot.RefreshTokenFromDisk()
	if !hasDiskToken {
		return nil, false
	}

	slog.Info("Found existing GitHub Copilot token on disk. Authenticating...")
	token, err := copilot.RefreshToken(context.TODO(), diskToken)
	if err != nil {
		slog.Error("Unable to import GitHub Copilot token", "error", err)
		return nil, false
	}

	if err := s.SetProviderAPIKey(ScopeGlobal, string(catwalk.InferenceProviderCopilot), token); err != nil {
		return token, false
	}

	if err := s.SetConfigFields(ScopeGlobal, map[string]any{
		"providers.copilot.api_key": token.AccessToken,
		"providers.copilot.oauth":   token,
	}); err != nil {
		slog.Error("Unable to save GitHub Copilot token to disk", "error", err)
	}

	slog.Info("GitHub Copilot successfully imported")
	return token, true
}

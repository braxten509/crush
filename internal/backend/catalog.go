package backend

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"charm.land/catwalk/v3/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/discover"
)

// ProviderCatalog lists the models a configured OpenRouter account (its own
// preferences applied) or OrcaRouter account offers that can call tools,
// for the app's "+ Add model" to save under it.
func (b *Backend) ProviderCatalog(ctx context.Context, workspaceID, providerID string) ([]catwalk.Model, error) {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}
	p, ok := ws.Cfg.Config().Providers.Get(providerID)
	if !ok || p.Disable {
		return nil, fmt.Errorf("%s is not set up", providerID)
	}
	p.ID = providerID
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	switch {
	case isOpenRouterProvider(p):
		return discover.OpenRouterCatalog(ctx, p.BaseURL, p.APIKey, p.ExtraHeaders, ws.Cfg.Resolver())
	case config.IsOrcaRouterProvider(p):
		return discover.OrcaRouterCatalog(ctx, p.BaseURL, p.APIKey, p.ExtraHeaders, ws.Cfg.Resolver())
	}
	return nil, fmt.Errorf("%s has no model catalog", providerID)
}

// isOpenRouterProvider reports whether p is an OpenRouter account. Catwalk
// v3 no longer has an OpenRouter provider type (a v2 "openrouter" type is
// read as plain chat completions), so it is told by the provider ID: the
// OpenRouter entry, another account copied from it ("openrouter-2"), or any
// entry that talks to openrouter.ai. A config that still carries the old
// type value counts too.
func isOpenRouterProvider(p config.ProviderConfig) bool {
	const id = string(catwalk.InferenceProviderOpenRouter)
	if p.ID == id || p.Type == catwalk.Type(id) {
		return true
	}
	if n, ok := strings.CutPrefix(p.ID, id+"-"); ok {
		if _, err := strconv.Atoi(n); err == nil {
			return true
		}
	}
	u, err := url.Parse(p.BaseURL)
	if err != nil {
		return false
	}
	host := u.Hostname()
	return host == "openrouter.ai" || strings.HasSuffix(host, ".openrouter.ai")
}

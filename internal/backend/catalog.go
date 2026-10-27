package backend

import (
	"context"
	"fmt"
	"time"

	"charm.land/catwalk/pkg/catwalk"
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
	case p.Type == catwalk.TypeOpenRouter:
		return discover.OpenRouterCatalog(ctx, p.BaseURL, p.APIKey, p.ExtraHeaders, ws.Cfg.Resolver())
	case config.IsOrcaRouterProvider(p):
		return discover.OrcaRouterCatalog(ctx, p.BaseURL, p.APIKey, p.ExtraHeaders, ws.Cfg.Resolver())
	}
	return nil, fmt.Errorf("%s has no model catalog", providerID)
}

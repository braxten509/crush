package backend

import (
	"context"
	"errors"
	"time"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/discover"
)

// openRouterProviderID is the configured provider the app saves picked
// OpenRouter models under.
const openRouterProviderID = "openrouter"

// OpenRouterCatalog lists the models the configured OpenRouter account may
// use (its own preferences applied) that can call tools.
func (b *Backend) OpenRouterCatalog(ctx context.Context, workspaceID string) ([]catwalk.Model, error) {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}
	p, ok := ws.Cfg.Config().Providers.Get(openRouterProviderID)
	if !ok || p.Type != catwalk.TypeOpenRouter {
		return nil, errors.New("OpenRouter is not set up")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return discover.OpenRouterCatalog(ctx, p.BaseURL, p.APIKey, p.ExtraHeaders, ws.Cfg.Resolver())
}

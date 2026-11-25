package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/crush/internal/accounts"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/fallback"
)

// AGY returns this error when the selected sign-in cannot start the turn.
func agyNeedsSignIn(err error) bool {
	return err != nil && strings.EqualFold(err.Error(), "agy: Authentication required")
}

// A rejected sign-in needs another account. Walk forward through the saved
// use blocks so another rejection cannot send the chat back to a failed one.
func (c *coordinator) nextAfterSignIn(ctx context.Context, start, cur config.SelectedModel) (config.FallbackBlock, string, bool) {
	cfg := c.cfg.Config()
	if c.providerKind(cur.Provider) != config.TypeAGYCLI || cfg.Options == nil || cfg.Options.Fallback == nil {
		return config.FallbackBlock{}, "", false
	}
	provider, model := fallback.Chosen(start)
	order := fallback.Keep(fallback.For(*cfg.Options.Fallback, provider, model), c.providerKind, cur, provider, model)
	at := fallback.Position(order, c.providerKind, cur)
	if at < 0 {
		return config.FallbackBlock{}, "", false
	}
	from := fallback.Account{Kind: config.TypeAGYCLI, ID: cur.Account}
	for i, b := range order.Blocks {
		if i <= at || b.Type != config.FallbackUse || b.KeepModel {
			continue
		}
		to := fallback.BlockAccount(b, c.providerKind)
		if to == from || fallback.Position(order, c.providerKind, fallback.Model(b)) <= at || !c.fallbackReady(ctx, b, to) {
			continue
		}
		line := fmt.Sprintf("Switched to %s · %s because %s needs to sign in.", accounts.Label(cfg, to.Kind, to.ID), c.modelName(b.Provider, b.Model), accounts.Label(cfg, from.Kind, from.ID))
		return b, line, true
	}
	return config.FallbackBlock{}, "", false
}

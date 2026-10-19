package backend

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/charmbracelet/crush/internal/agent/hyper"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/proto"
)

// ErrHyperCredits wraps a credits fetch that failed.
var ErrHyperCredits = errors.New("hyper credits")

// HyperCredits fetches the Hyper credits balance as the terminal does
// (internal/ui/model/polling.go): with Hyper's resolved key, after
// refreshing an expired OAuth token, from hyper.FetchCredits. Credits is
// nil when Hyper is not configured, or when the account shows no balance
// (desktop design 5-6, G-25).
func (b *Backend) HyperCredits(ctx context.Context) (proto.HyperCredits, error) {
	var ws *Workspace
	for _, w := range b.workspaces.Seq2() {
		if w.Cfg != nil {
			ws = w
			break
		}
	}
	if ws == nil {
		return proto.HyperCredits{}, nil
	}
	var (
		apiKey      string
		providerCfg config.ProviderConfig
	)
	getAPIKey := func() bool {
		cfg := ws.Cfg.Config()
		if cfg == nil || cfg.Providers == nil {
			return false
		}
		var ok bool
		if providerCfg, ok = cfg.Providers.Get(hyper.Name); !ok {
			return false
		}
		var err error
		apiKey, err = ws.Cfg.Resolver().ResolveValue(providerCfg.APIKey)
		return err == nil && apiKey != ""
	}
	if !getAPIKey() {
		return proto.HyperCredits{}, nil
	}
	if providerCfg.OAuthToken != nil && providerCfg.OAuthToken.IsExpired() {
		ctxRefresh, cancelRefresh := context.WithTimeout(ctx, 15*time.Second)
		defer cancelRefresh()
		if err := ws.ops().RefreshOAuthToken(ctxRefresh, config.ScopeGlobal, hyper.Name); err != nil {
			slog.Warn("Hyper OAuth refresh failed before fetching credits, trying with existing token", "error", err)
		} else if !getAPIKey() {
			return proto.HyperCredits{}, nil
		}
	}
	ctxFetch, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	credits, err := hyper.FetchCredits(ctxFetch, apiKey)
	if err != nil {
		return proto.HyperCredits{}, kindErr(ErrHyperCredits, err)
	}
	return proto.HyperCredits{Credits: credits}, nil
}

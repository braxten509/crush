package backend

import (
	"context"
	"log/slog"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/proto"
)

// providerSources are the places RefreshProviders reads, by source; ""
// is `crush update-providers`' default. Tests point them at local servers.
var providerSources = map[string]string{"catwalk": "", "hyper": ""}

// SetProviderSourcesForTest points the provider refresh at catwalk and
// hyper (URLs or files) and returns a function that restores the defaults.
func SetProviderSourcesForTest(catwalk, hyper string) func() {
	prev := providerSources
	providerSources = map[string]string{"catwalk": catwalk, "hyper": hyper}
	return func() { providerSources = prev }
}

// RefreshProviders runs what `crush update-providers` runs for each of
// its sources (Catwalk, then Hyper), from their default places, then
// reloads every workspace's config so the new catalogs show at once and
// sends config_changed (desktop design 5-6, 2.3; G-9).
func (b *Backend) RefreshProviders(ctx context.Context) proto.ProvidersRefresh {
	out := proto.ProvidersRefresh{Results: []proto.ProviderRefreshResult{}}
	for _, source := range []struct {
		name   string
		update func(string) error
	}{
		{"catwalk", config.UpdateProviders},
		{"hyper", config.UpdateHyper},
	} {
		result := proto.ProviderRefreshResult{Source: source.name, OK: true}
		if err := source.update(providerSources[source.name]); err != nil {
			result.OK, result.Error = false, err.Error()
		}
		out.Results = append(out.Results, result)
	}
	if err := config.ReloadModelCatalog(); err != nil {
		slog.Warn("Failed to reload the provider catalog", "error", err)
	}
	for _, ws := range b.workspaces.Seq2() {
		if ws.Cfg == nil {
			continue
		}
		if err := ws.Cfg.ReloadFromDisk(ctx); err != nil {
			slog.Warn("Failed to reload config after a provider refresh", "workspace", ws.ID, "error", err)
		}
		sendConfigChanged(ws)
	}
	return out
}

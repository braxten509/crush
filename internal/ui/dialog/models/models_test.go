package models

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/textinput"
	"charm.land/catwalk/v3/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/csync"
	"github.com/charmbracelet/crush/internal/oauth"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/charmbracelet/crush/internal/workspace"
	"github.com/stretchr/testify/require"
)

type modelsWorkspace struct {
	workspace.Workspace
	cfg    *config.Config
	fields map[string]any
}

func (w *modelsWorkspace) Config() *config.Config { return w.cfg }

func (w *modelsWorkspace) SetConfigField(_ config.Scope, key string, value any) error {
	w.fields[key] = value
	return nil
}

// TestSetProviderItems checks the groups and items the models dialog builds
// from the configured and known providers: custom providers, configured
// name overrides, disabled providers, the shown chat's model selected, and
// the recently used list with its stale entries pruned and saved.
func TestSetProviderItems(t *testing.T) {
	// The known provider catalog is loaded once per process from the
	// embedded copy; keep its cache out of the user's directories.
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	cfg := &config.Config{
		Options: &config.Options{DisableProviderAutoUpdate: true},
		RecentModels: map[config.SelectedModelType][]config.SelectedModel{
			config.SelectedModelTypeChat: {
				{Provider: "openai", Model: "sub-1"},
				{Provider: "gone", Model: "nope"},
				{Provider: "anthropic", Model: "claude-a"},
			},
		},
		Providers: csync.NewMapFrom(map[string]config.ProviderConfig{
			"openai": {
				Name:          "My OpenAI",
				OAuthToken:    &oauth.Token{AccessToken: "x"},
				ChatGPTModels: []catwalk.Model{{ID: "sub-1", Name: "Sub One"}, {ID: "sub-2", Name: "Sub Two"}},
			},
			"anthropic": {
				Models: []catwalk.Model{
					{ID: "claude-a", Name: "Renamed A"},
					{ID: "claude-extra"},
					{ID: ""},
				},
			},
			"xai":    {Disable: true},
			"custom": {ID: "custom", Name: "Custom", Models: []catwalk.Model{{ID: "c-1", Name: "C One"}}},
		}),
	}
	ws := &modelsWorkspace{cfg: cfg, fields: map[string]any{}}
	sty := styles.CharmtonePantera()
	m := &Models{
		com: &common.Common{Workspace: ws, Styles: &sty, ShownModel: func() (config.SelectedModel, bool) {
			// The shown chat's own model is the one marked.
			return config.SelectedModel{Provider: "anthropic", Model: "claude-b"}, true
		}},
		list:      NewModelsList(&sty),
		input:     textinput.New(),
		modelType: ModelTypeChat,
		providers: []catwalk.Provider{
			{ID: "openai", Name: "OpenAI", Models: []catwalk.Model{{ID: "gpt-a", Name: "GPT A"}}},
			{ID: "anthropic", Name: "Anthropic", Models: []catwalk.Model{{ID: "claude-a", Name: "Claude A"}, {ID: "claude-b", Name: "Claude B"}}},
			{ID: "xai", Name: "xAI", Models: []catwalk.Model{{ID: "grok-a", Name: "Grok A"}}},
			{ID: "gemini", Name: "Gemini", Models: []catwalk.Model{{ID: "gem-a", Name: "Gem A"}}},
		},
	}
	require.NoError(t, m.setProviderItems())

	// The stale recent entry is pruned and the rest saved.
	require.Len(t, ws.fields, 1)
	for _, value := range ws.fields {
		var saved []string
		for _, sm := range value.([]config.SelectedModel) {
			saved = append(saved, sm.Provider+":"+sm.Model)
		}
		require.Equal(t, []string{"openai:sub-1", "anthropic:claude-a"}, saved)
	}

	var titles []string
	for _, g := range m.list.groups {
		titles = append(titles, g.Title)
		for _, it := range g.Items {
			require.False(t, strings.HasPrefix(it.ID(), "xai:"), "disabled providers are not listed")
			if it.ID() == "anthropic:claude-a" {
				require.Equal(t, "Renamed A", it.model.Name, "configured names win")
			}
		}
	}
	require.Contains(t, titles, "Custom")

	sel, ok := m.list.SelectedItem().(*ModelItem)
	require.True(t, ok)
	require.Equal(t, "anthropic:claude-b", sel.ID())
}

// TestWithConfiguredModelsLeavesCatalogAlone pins that configured model
// names and extra models never write through into the catalog's models.
func TestWithConfiguredModelsLeavesCatalogAlone(t *testing.T) {
	t.Parallel()

	catalogModels := make([]catwalk.Model, 1, 4)
	catalogModels[0] = catwalk.Model{ID: "a", Name: "Catalog A"}
	catalog := catwalk.Provider{ID: "p", Name: "P", Models: catalogModels}

	got := withConfiguredModels(catalog, config.ProviderConfig{
		Models: []catwalk.Model{{ID: "a", Name: "Configured A"}, {ID: "b"}},
	})
	require.Equal(t, []catwalk.Model{{ID: "a", Name: "Configured A"}, {ID: "b", Name: "b"}}, got.Models)
	require.Equal(t, "Catalog A", catalog.Models[0].Name)
	require.Equal(t, catwalk.Model{}, catalogModels[:2][1], "appending must not fill the catalog's spare capacity")
}

// TestSetProviderItemsKeepsCatalogNames pins that building the groups twice
// shows catalog names again once the configured override is gone.
func TestSetProviderItemsKeepsCatalogNames(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	cfg := &config.Config{
		Options: &config.Options{DisableProviderAutoUpdate: true},
		Models:  map[config.SelectedModelType]config.SelectedModel{},
		Providers: csync.NewMapFrom(map[string]config.ProviderConfig{
			"anthropic": {Models: []catwalk.Model{{ID: "claude-a", Name: "Renamed A"}}},
		}),
	}
	sty := styles.CharmtonePantera()
	m := &Models{
		com:       &common.Common{Workspace: &modelsWorkspace{cfg: cfg, fields: map[string]any{}}, Styles: &sty},
		list:      NewModelsList(&sty),
		input:     textinput.New(),
		modelType: ModelTypeChat,
		providers: []catwalk.Provider{
			{ID: "anthropic", Name: "Anthropic", Models: []catwalk.Model{{ID: "claude-a", Name: "Claude A"}}},
		},
	}
	require.NoError(t, m.setProviderItems())
	require.Equal(t, "Claude A", m.providers[0].Models[0].Name)

	cfg.Providers.Set("anthropic", config.ProviderConfig{})
	require.NoError(t, m.setProviderItems())
	require.Equal(t, "Claude A", m.list.groups[0].Items[0].model.Name)
}

// TestCustomProviderGroupsAreSorted pins that providers missing from the
// catalog get their groups in name order, not the config map's order.
func TestCustomProviderGroupsAreSorted(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	providers := map[string]config.ProviderConfig{}
	var want []string
	for _, name := range []string{"Alpha", "Bravo", "Charlie", "Delta", "Echo", "Foxtrot", "Golf", "Hotel"} {
		id := "custom-" + strings.ToLower(name)
		providers[id] = config.ProviderConfig{ID: id, Name: name, Models: []catwalk.Model{{ID: id + "-model"}}}
		want = append(want, name)
	}
	// Without a name the ID is shown, and sorts by it.
	providers["aaa"] = config.ProviderConfig{ID: "aaa", Models: []catwalk.Model{{ID: "aaa-model"}}}
	want = append(want, "aaa")

	cfg := &config.Config{
		Options:   &config.Options{DisableProviderAutoUpdate: true},
		Models:    map[config.SelectedModelType]config.SelectedModel{},
		Providers: csync.NewMapFrom(providers),
	}
	sty := styles.CharmtonePantera()
	m := &Models{
		com:       &common.Common{Workspace: &modelsWorkspace{cfg: cfg, fields: map[string]any{}}, Styles: &sty},
		list:      NewModelsList(&sty),
		input:     textinput.New(),
		modelType: ModelTypeChat,
	}
	for range 5 {
		require.NoError(t, m.setProviderItems())
		var got []string
		for _, g := range m.list.groups {
			got = append(got, g.Title)
		}
		require.Equal(t, want, got)
	}
}

func TestWithConfiguredModelsReplacesCatalogWhenDynamic(t *testing.T) {
	t.Parallel()

	catalog := catwalk.Provider{
		ID:     "anthropic",
		Name:   "Anthropic",
		Models: []catwalk.Model{{ID: "fixed-a", Name: "Fixed A"}, {ID: "fixed-b", Name: "Fixed B"}},
	}
	got := withConfiguredModels(catalog, config.ProviderConfig{
		DynamicModels: true,
		Models:        []catwalk.Model{{ID: "live-1", Name: "Live One"}},
	})
	require.Equal(t, []catwalk.Model{{ID: "live-1", Name: "Live One"}}, got.Models)
}

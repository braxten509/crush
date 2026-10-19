package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/charmbracelet/crush/internal/backend"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/db"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// g56Workspace is a server with one real workspace created through the
// HTTP route, and one app on it. Nothing reaches the network: no agent
// starts and the one MCP server is disabled.
type g56Workspace struct {
	h   *e2eHarness
	ws  proto.Workspace
	app string
}

// newG56Workspace creates the workspace. Like newRealCreateHarness it
// isolates HOME and the XDG folders, so callers must not be parallel.
func newG56Workspace(t *testing.T) *g56Workspace {
	t.Helper()
	h := newRealCreateHarness(t)
	t.Setenv("CRUSH_DISABLE_PROVIDER_AUTO_UPDATE", "1")
	dir := t.TempDir()
	cfg, err := json.Marshal(map[string]any{
		"providers": map[string]any{"fake": map[string]any{
			"type": "openai-compat", "base_url": "http://127.0.0.1:9/v1", "api_key": "test-key",
			"models": []map[string]any{{"id": "fake-model", "name": "Fake model", "context_window": 200000, "default_max_tokens": 256}},
		}},
		"models": map[string]any{
			"large": map[string]any{"provider": "fake", "model": "fake-model"},
			"small": map[string]any{"provider": "fake", "model": "fake-model"},
		},
		"mcp": map[string]any{"probe": map[string]any{"type": "stdio", "command": "/nonexistent/probe", "disabled": true}},
		"options": map[string]any{
			"disable_auto_summarize": true, "disable_metrics": true, "disable_provider_auto_update": true,
			"auto_lsp": false, "notifications": "disabled",
		},
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "crush.json"), cfg, 0o600))

	app := uuid.NewString()
	ws := h.postWorkspace(t, proto.Workspace{Path: dir, DataDir: t.TempDir(), ClientID: app})
	t.Cleanup(func() { _ = h.backend.RetireClient(app) })
	return &g56Workspace{h: h, ws: ws, app: app}
}

// path is the workspace's route prefix plus rest.
func (g *g56Workspace) path(rest string) string {
	return "/v1/workspaces/" + g.ws.ID + rest
}

// TestSkipAndMCPTogglesPublishConfigChanged: the skip switch and both MCP
// toggles send config_changed (G-4).
func TestSkipAndMCPTogglesPublishConfigChanged(t *testing.T) {
	g := newG56Workspace(t)
	events := g.h.subscribeLiveSSE(t, g.ws.ID, g.app)
	g.h.waitForAttachedOn(t, mustWorkspace(t, g), 1)

	for _, step := range []struct {
		name, path string
		body       any
	}{
		{"skip", "/permissions/skip", proto.PermissionSkipRequest{Skip: true}},
		{"mcp disabled", "/mcp/disabled", proto.MCPSetServerDisabledRequest{Name: "probe", Disabled: true}},
		{"mcp config-disabled", "/mcp/config-disabled", proto.MCPSetServerDisabledRequest{Name: "probe", Disabled: true}},
	} {
		liveJSON[struct{}](t, g.h, http.MethodPost, g.path(step.path), step.body, http.StatusOK)
		ev, ok := nextLive[proto.ConfigChanged](t, events, pubsub.PayloadTypeConfigChanged, 5*time.Second)
		require.True(t, ok, "%s must send config_changed", step.name)
		require.Equal(t, g.ws.ID, ev.Payload.WorkspaceID)
	}
}

// TestConfigWatchPublishesOnOutsideWrite: a config file another engine (or
// a hand edit) changed is reloaded within seconds and the apps get
// config_changed (G-1).
func TestConfigWatchPublishesOnOutsideWrite(t *testing.T) {
	g := newG56Workspace(t)
	events := g.h.subscribeLiveSSE(t, g.ws.ID, g.app)
	g.h.waitForAttachedOn(t, mustWorkspace(t, g), 1)
	compact := func() bool {
		tui := mustWorkspace(t, g).Cfg.Config().Options.TUI
		return tui != nil && tui.CompactMode
	}
	require.False(t, compact())

	path := config.GlobalConfigData()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(`{"options":{"tui":{"compact_mode":true}}}`), 0o600))

	ev, ok := nextLive[proto.ConfigChanged](t, events, pubsub.PayloadTypeConfigChanged, 5*time.Second)
	require.True(t, ok, "an outside write sends config_changed")
	require.Equal(t, g.ws.ID, ev.Payload.WorkspaceID)
	require.True(t, compact(), "the engine reloaded the file")
}

// TestConfigWatchSeesThemeFile: a theme file written by another app sends
// config_changed (G-1).
func TestConfigWatchSeesThemeFile(t *testing.T) {
	g := newG56Workspace(t)
	events := g.h.subscribeLiveSSE(t, g.ws.ID, g.app)
	g.h.waitForAttachedOn(t, mustWorkspace(t, g), 1)
	// Let the watch take its first look at the themes folder.
	time.Sleep(1500 * time.Millisecond)

	path, err := styles.ThemePath("outside")
	require.NoError(t, err)
	require.NoError(t, styles.SaveThemeFile(path, &styles.ThemeFile{Base: "graphite", Palette: styles.Palette{BgBase: "#101010"}}))

	_, ok := nextLive[proto.ConfigChanged](t, events, pubsub.PayloadTypeConfigChanged, 5*time.Second)
	require.True(t, ok, "a new theme file sends config_changed")
}

// mustWorkspace returns the backend's record of g's workspace.
func mustWorkspace(t *testing.T, g *g56Workspace) *backend.Workspace {
	t.Helper()
	ws, err := g.h.backend.GetWorkspace(g.ws.ID)
	require.NoError(t, err)
	return ws
}

// TestWorkspaceCarriesConfigPath: the workspace record names the global
// data config file (G-6).
func TestWorkspaceCarriesConfigPath(t *testing.T) {
	g := newG56Workspace(t)
	require.Equal(t, config.GlobalConfigData(), g.ws.ConfigPath, "the create answer carries config_path")
	got := liveJSON[proto.Workspace](t, g.h, http.MethodGet, g.path(""), nil, http.StatusOK)
	require.Equal(t, config.GlobalConfigData(), got.ConfigPath, "GET workspace carries config_path")
}

// TestDraftCleanupSkipsShownDrafts: listing saved chats discards old empty
// drafts, but never one another app shows (G-11).
func TestDraftCleanupSkipsShownDrafts(t *testing.T) {
	g := newG56Workspace(t)
	other := uuid.NewString()
	g.h.subscribeLiveSSE(t, g.ws.ID, g.app)
	g.h.subscribeLiveSSE(t, g.ws.ID, other)
	g.h.waitForAttachedOn(t, mustWorkspace(t, g), 2)

	shown := liveJSON[proto.Session](t, g.h, http.MethodPost, g.path("/sessions"), proto.Session{Title: "shown"}, http.StatusOK)
	hidden := liveJSON[proto.Session](t, g.h, http.MethodPost, g.path("/sessions"), proto.Session{Title: "hidden"}, http.StatusOK)
	conn, err := db.Connect(t.Context(), g.ws.DataDir)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Release(g.ws.DataDir) })
	old := time.Now().Add(-time.Hour).Unix()
	_, err = conn.ExecContext(t.Context(), "UPDATE sessions SET created_at=? WHERE id IN (?, ?)", old, shown.ID, hidden.ID)
	require.NoError(t, err)
	g.h.setCurrentSession(t, t.Context(), g.ws.ID, other, shown.ID)

	liveJSON[[]proto.Session](t, g.h, http.MethodGet, g.path("/saved-sessions"), nil, http.StatusOK)
	exists := func(id string) bool {
		var found bool
		require.NoError(t, conn.QueryRowContext(t.Context(), "SELECT EXISTS(SELECT 1 FROM sessions WHERE id=?)", id).Scan(&found))
		return found
	}
	require.True(t, exists(shown.ID), "a draft another app shows is kept")
	require.False(t, exists(hidden.ID), "an old empty draft nobody shows is still discarded")
}

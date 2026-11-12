//go:build crushtest

package cmd

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/charmbracelet/crush/internal/backend"
	"github.com/charmbracelet/crush/internal/db"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/proto/prototest"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// useSeams installs the seams in body, read from a file as crush reads
// CRUSH_TEST_SEAMS, until the test ends.
func useSeams(t *testing.T, body string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	path := filepath.Join(t.TempDir(), "seams.json")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	restore, err := loadTestSeams(path)
	require.NoError(t, err)
	t.Cleanup(restore)
}

func newSeamBackend(t *testing.T) *backend.Backend {
	t.Helper()
	return backend.New(context.Background(), nil, func() {})
}

// seamWorkspace creates a project workspace on b, as an app's first call
// does, with a provider that is never reached.
func seamWorkspace(t *testing.T, b *backend.Backend) *backend.Workspace {
	t.Helper()
	t.Setenv("CRUSH_GLOBAL_CONFIG", t.TempDir())
	t.Setenv("CRUSH_GLOBAL_DATA", t.TempDir())
	t.Setenv("CRUSH_DISABLE_PROVIDER_AUTO_UPDATE", "1")
	dir := t.TempDir()
	cfg, err := json.Marshal(map[string]any{
		"providers": map[string]any{"fake": map[string]any{
			"type": "openai-compat", "base_url": "http://127.0.0.1:9/v1", "api_key": "test-key",
			"models": []map[string]any{{"id": "fake-model", "name": "Fake model", "context_window": 200000, "default_max_tokens": 256}},
		}},
		"models":  map[string]any{"large": map[string]any{"provider": "fake", "model": "fake-model"}},
		"options": map[string]any{"disable_metrics": true, "disable_provider_auto_update": true, "auto_lsp": false, "notifications": "disabled"},
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "crush.json"), cfg, 0o600))
	app := uuid.NewString()
	ws, _, err := b.CreateWorkspace(prototest.CreateRequest(proto.Workspace{Path: dir, DataDir: t.TempDir(), ClientID: app}))
	require.NoError(t, err)
	dataDir := ws.Cfg.Config().Options.DataDirectory
	backend.SetWorkspaceShutdownFnForTest(ws, func() { _ = db.Release(dataDir) })
	t.Cleanup(func() { _ = b.RetireClient(app) })
	return ws
}

// TestSeamAuth: the auth seam answers key checks and runs each sign-in
// with its URL and code, then saves the stand-in token as a real sign-in
// does (G-24).
func TestSeamAuth(t *testing.T) {
	useSeams(t, `{"auth":{
		"verify_key":{"fake":"ok","openai":"failed to connect to provider openai: 401 Unauthorized"},
		"flows":{"copilot":{"url":"https://github.com/login/device","user_code":"ABCD-1234","result":"ok","after_ms":50}}}}`)
	b := newSeamBackend(t)
	ws := seamWorkspace(t, b)

	ok, err := b.VerifyAPIKey(ws.ID, "fake", "any-key")
	require.NoError(t, err)
	require.Equal(t, proto.VerifyKey{OK: true}, ok)
	bad, err := b.VerifyAPIKey(ws.ID, "openai", "any-key")
	require.NoError(t, err)
	require.Equal(t, proto.VerifyKey{Error: "failed to connect to provider openai: 401 Unauthorized"}, bad)

	flow, err := b.StartAuthFlow(ws.ID, "copilot")
	require.NoError(t, err)
	require.Equal(t, "https://github.com/login/device", flow.URL)
	require.Equal(t, "ABCD-1234", flow.UserCode)
	require.Eventually(t, func() bool {
		pc, found := ws.Cfg.Config().Providers.Get("copilot")
		return found && pc.OAuthToken != nil && pc.OAuthToken.AccessToken == "crushtest-access"
	}, 10*time.Second, 20*time.Millisecond, "the sign-in saved its token")

	_, err = b.StartAuthFlow(ws.ID, "hyper")
	require.Error(t, err, "a platform the seam leaves out starts no real sign-in")
	saved, err := os.ReadFile(ws.Cfg.GlobalDataPath())
	require.NoError(t, err)
	require.Contains(t, string(saved), "crushtest-refresh", "saved in the global data config, as crush login saves it")
}

// TestSeamAuthCancel: a sign-in that was cancelled, or ran out of time,
// saves nothing, even when its provider hands over a token after that
// (G-5, through the auth seam's late flows). The time-out is only watched
// for, through the flow's own events, never caused by the test.
func TestSeamAuthCancel(t *testing.T) {
	useSeams(t, `{"auth":{"timeout_ms":300,"flows":{
		"copilot":{"url":"https://github.com/login/device","user_code":"ABCD-1234","result":"ok","after_ms":60000,"late":true},
		"openai":{"url":"https://auth.openai.com/authorize","result":"ok","after_ms":60000,"late":true}}}}`)
	b := newSeamBackend(t)
	ws := seamWorkspace(t, b)

	cancelled, err := b.StartAuthFlow(ws.ID, "copilot")
	require.NoError(t, err)
	require.NoError(t, b.CancelAuthFlow(cancelled.FlowID))
	require.ErrorIs(t, b.CancelAuthFlow(cancelled.FlowID), backend.ErrAuthFlowNotFound, "the flow is gone")

	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	events := ws.App.Events(ctx)
	expired, err := b.StartAuthFlow(ws.ID, "openai")
	require.NoError(t, err)
	timedOut := false
	for deadline := time.After(10 * time.Second); !timedOut; {
		select {
		case ev := <-events:
			if e, ok := ev.Payload.(pubsub.Event[proto.AuthFlowEvent]); ok && e.Payload.FlowID == expired.FlowID {
				require.NotEqual(t, proto.AuthFlowSaving, e.Payload.State, "a timed-out sign-in does not save")
				timedOut = e.Payload.State == proto.AuthFlowFailed
			}
		case <-deadline:
			t.Fatal("no time-out reported")
		}
	}

	time.Sleep(300 * time.Millisecond) // the late tokens have been handed over by now
	for _, id := range []string{"copilot", "openai"} {
		pc, found := ws.Cfg.Config().Providers.Get(id)
		require.False(t, found && pc.OAuthToken != nil, "%s: a token handed over after the end is not saved", id)
	}
}

//go:build crushtest

package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/agent/cliagent"
	"github.com/charmbracelet/crush/internal/backend"
	"github.com/charmbracelet/crush/internal/cliupdate"
	"github.com/charmbracelet/crush/internal/db"
	"github.com/charmbracelet/crush/internal/memwatch"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/proto/prototest"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/charmbracelet/crush/internal/remote"
	"github.com/charmbracelet/crush/internal/skills"
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

// TestSeamMemory: the memory seam makes the engine report the given
// memory state (G-21).
func TestSeamMemory(t *testing.T) {
	if os.Getenv(seamStartChildEnv) == "1" {
		// The child: crush starts as it always does, and reads the seams
		// file CRUSH_TEST_SEAMS names on the way.
		os.Args = []string{"crush", "--version"}
		Execute()
		st := newSeamBackend(t).MemoryStatus()
		require.Equal(t, 93, st.Percent)
		require.True(t, st.High)
		require.Equal(t, uint64(17179869184), st.TotalRAMBytes)
		require.Equal(t, []proto.MemoryProgram{{Name: "firefox", Bytes: 3221225472}, {Name: "crush", Bytes: 1073741824}}, st.Top)
		require.Equal(t, uint64(17179869184), memwatch.TotalRAM())
		return
	}
	path := filepath.Join(t.TempDir(), "seams.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"memory":{"percent":93,"top":[{"name":"firefox","bytes":3221225472},{"name":"crush","bytes":1073741824}],"total_ram_bytes":17179869184}}`), 0o600))
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSeamMemory$", "-test.v")
	cmd.Env = append(os.Environ(), seamStartChildEnv+"=1", testSeamsEnv+"="+path, "HOME="+t.TempDir())
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", out)
	require.Contains(t, string(out), "--- PASS: TestSeamMemory")
}

// seamStartChildEnv runs TestSeamMemory's check in a child process that
// starts crush through Execute, the real startup path.
const seamStartChildEnv = "CRUSH_G56_SEAM_START_CHILD"

// TestSeamUpdates: the cli_updates and crush_release seams answer the
// update checks and installs with no CLI run and no network (G-22).
func TestSeamUpdates(t *testing.T) {
	useSeams(t, `{
		"cli_updates":[
			{"name":"Codex","bin":"codex","path":"/opt/codex","current":"1.0.0","latest":"1.1.0","install":"ok"},
			{"name":"Grok","bin":"grok","path":"/opt/grok","current":"2.0.0","latest":"2.1.0","install":"network down"}],
		"crush_release":{"tag":"v9.0.0","current":"v8.0.0","dir":"/tmp/crush-fork","staged":"v9.0.0-local.20261019-fork"}}`)
	b := newSeamBackend(t)
	check := b.CheckCLIUpdates(t.Context(), true)
	require.Empty(t, check.Errors)
	require.Equal(t, []proto.CLIUpdate{
		{Name: "Codex", Bin: "codex", Path: "/opt/codex", Current: "1.0.0", Latest: "1.1.0"},
		{Name: "Grok", Bin: "grok", Path: "/opt/grok", Current: "2.0.0", Latest: "2.1.0"},
	}, check.Updates)
	updates, _ := cliupdate.Check(t.Context())
	require.Len(t, updates, 2)
	require.NoError(t, cliupdate.Install(t.Context(), updates[0]))
	require.EqualError(t, cliupdate.Install(t.Context(), updates[1]), "Grok: network down")

	crush := b.CheckCrushUpdate(t.Context())
	require.True(t, crush.HasCheckout)
	require.Equal(t, &proto.CrushRelease{Tag: "v9.0.0", Current: "v8.0.0", Dir: "/tmp/crush-fork"}, crush.Release)
	require.Equal(t, "v9.0.0-local.20261019-fork", crush.Staged)
}

// TestSeamRemoteLoopback: the remote seam stands in for Tailscale; the
// loopback address counts as this computer's tailnet address, so the
// launcher listens and answers there, with the seam's host (G-23).
func TestSeamRemoteLoopback(t *testing.T) {
	useSeams(t, `{"remote":{"listen":"127.0.0.1","host":"crushtest-pc","user":"tester@example.com","phones":["Pixel"]}}`)
	require.Equal(t, remote.SetupReady, remote.CheckSetup(t.Context()).State)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		done <- remote.RunLauncher(ctx, filepath.Join(t.TempDir(), "remote-password"), func(string) (*exec.Cmd, error) {
			return nil, fmt.Errorf("no window in a test")
		})
	}()
	t.Cleanup(func() {
		cancel()
		require.NoError(t, <-done)
	})
	url := fmt.Sprintf("http://127.0.0.1:%d/v1/launcher", remote.LauncherPort)
	var info struct {
		Host        string `json:"host"`
		PasswordSet bool   `json:"password_set"`
	}
	require.Eventually(t, func() bool {
		resp, err := http.Get(url) //nolint:noctx
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		return resp.StatusCode == http.StatusOK && json.NewDecoder(resp.Body).Decode(&info) == nil
	}, 10*time.Second, 50*time.Millisecond, "the launcher answers on loopback")
	require.Equal(t, "crushtest-pc", info.Host)
	require.False(t, info.PasswordSet)
}

// TestSeamLimits: the limits seam replaces every usage fetcher (G-24).
func TestSeamLimits(t *testing.T) {
	useSeams(t, `{"limits":{"claude-cli":[{"name":"5h","used":40,"resets_at":"2030-01-01T00:00:00Z"},{"name":"Weekly","used":75}],"codex-cli":"not signed in"}}`)
	require.True(t, cliagent.LimitFetchersReplaced())
	require.NoError(t, cliagent.RefreshLimitsNow(t.Context(), catwalk.Type("claude-cli")))
	require.EqualError(t, cliagent.RefreshLimitsNow(t.Context(), catwalk.Type("codex-cli")), "not signed in")

	snapshot := cliagent.Snapshot()
	require.Len(t, snapshot, 2)
	byKind := map[catwalk.Type]cliagent.KindLimits{}
	for _, k := range snapshot {
		byKind[k.Kind] = k
	}
	require.Equal(t, []cliagent.Limit{
		{Name: "5h", Used: 40, ResetsAt: time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)},
		{Name: "Weekly", Used: 75},
	}, byKind["claude-cli"].Limits)
	require.Equal(t, "not signed in", byKind["codex-cli"].Err)
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

// TestSeamAuthGrokCode: a Grok sign-in from the auth seam takes a pasted
// code; the right one finishes it and the token is saved (G-24).
func TestSeamAuthGrokCode(t *testing.T) {
	useSeams(t, `{"auth":{"flows":{"grok":{"url":"https://accounts.x.ai/oauth","result":"ok","code":"G56-CODE"}}}}`)
	b := newSeamBackend(t)
	ws := seamWorkspace(t, b)

	flow, err := b.StartAuthFlow(ws.ID, "grok")
	require.NoError(t, err)
	require.True(t, flow.CodeEntry)
	require.NoError(t, b.SendAuthCode(flow.FlowID, "G56-CODE"))
	require.Eventually(t, func() bool {
		pc, found := ws.Cfg.Config().Providers.Get("xai")
		return found && pc.OAuthToken != nil && pc.OAuthToken.AccessToken == "crushtest-access"
	}, 10*time.Second, 20*time.Millisecond, "the pasted code finished the sign-in")
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
				timedOut = e.Payload.State == proto.AuthFlowFailed && e.Payload.Error == "authentication timed out"
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

// TestSeamSkillsSearch: the skills_search seam answers skills.sh searches
// by query; other queries find nothing (G-24).
func TestSeamSkillsSearch(t *testing.T) {
	useSeams(t, `{"skills_search":{"pdf":[{"id":"acme/tools/pdf","name":"pdf","source":"acme/tools","skillId":"pdf","installs":42}]}}`)
	b := newSeamBackend(t)
	ws := seamWorkspace(t, b)

	found, err := b.SearchSkills(t.Context(), ws.ID, "pdf")
	require.NoError(t, err)
	require.Equal(t, []skills.DirectorySkill{{ID: "acme/tools/pdf", Name: "pdf", Source: "acme/tools", SkillID: "pdf", Installs: 42}}, found)
	none, err := b.SearchSkills(t.Context(), ws.ID, "nothing here")
	require.NoError(t, err)
	require.Empty(t, none)
}

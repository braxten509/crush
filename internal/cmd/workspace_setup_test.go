//go:build !windows

package cmd

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/charmbracelet/crush/internal/client"
	"github.com/charmbracelet/crush/internal/db"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/proto/prototest"
	"github.com/charmbracelet/crush/internal/serverctl"
	"github.com/stretchr/testify/require"
)

// TestClientServerEnvJoinsEngineWorkspace: with CRUSH_CLIENT_SERVER=1 and
// no --host, connectToServer starts the project's engine at its engine
// socket and joins its workspace: a second client created on that engine
// directly gets the same workspace ID. The legacy server is not used.
func TestClientServerEnvJoinsEngineWorkspace(t *testing.T) {
	p := newCmdProject(t)
	t.Setenv("CRUSH_CLIENT_SERVER", "1")
	t.Chdir(p.cwd)
	legacySocket := defaultSocket(t)

	_, ws, cleanup, err := connectToServer(setupCmd(t))
	require.NoError(t, err)
	t.Cleanup(cleanup)

	h, ok, err := serverctl.FindEngine(t.Context(), serverctl.EnsureOptions{Cwd: p.cwd})
	require.NoError(t, err)
	require.True(t, ok, "the project's engine runs")
	p.track(h.PID)
	require.Equal(t, p.socket, h.Socket)

	direct, err := client.NewClient(p.cwd, "unix", p.socket)
	require.NoError(t, err)
	other, err := direct.CreateWorkspace(t.Context(), prototest.CreateRequest(proto.Workspace{Path: p.cwd}))
	require.NoError(t, err)
	t.Cleanup(func() { _ = direct.RetireClient(context.Background()) })
	require.Equal(t, ws.ID, other.ID, "one workspace on the engine")
	require.NoFileExists(t, legacySocket, "the legacy server is not used")
}

// TestExplicitHostKeepsLegacyServer: an explicit --host takes the legacy
// server even with the switch on; so does connectToServer with the switch
// off and no --host when no engine runs, as `crush logout` calls it, on
// the default host, looked up only then. No engine starts in either case.
func TestExplicitHostKeepsLegacyServer(t *testing.T) {
	p := newCmdProject(t)
	t.Chdir(p.cwd)

	t.Setenv("CRUSH_CLIENT_SERVER", "1")
	explicit := filepath.Join(p.root, "x.sock")
	c, ws, cleanup, err := connectToServer(setupCmd(t, "--host", "unix://"+explicit))
	require.NoError(t, err)
	require.NotEmpty(t, ws.ID)
	require.FileExists(t, explicit, "the legacy server listens on --host")
	cleanup()
	stopLegacyServer(t, c, explicit, p.dataDir)
	require.NoFileExists(t, p.socket, "no engine started")

	t.Setenv("CRUSH_CLIENT_SERVER", "")
	fallback := defaultSocket(t)
	c, ws, cleanup, err = connectToServer(setupCmd(t))
	require.NoError(t, err)
	require.NotEmpty(t, ws.ID)
	require.FileExists(t, fallback, "the legacy server listens on the default host")
	cleanup()
	stopLegacyServer(t, c, fallback, p.dataDir)
	require.NoFileExists(t, p.socket, "FindEngine starts no engine")
}

// TestLogoutJoinsLiveEngine: a real engine holds the project, as the
// desktop app leaves it, and the switch is off. `crush logout --force
// hyper` joins that engine instead of meeting its lock through the legacy
// server: it succeeds, the engine's loaded config forgets the sign-in, no
// legacy server socket appears, and the engine's workspace stays.
func TestLogoutJoinsLiveEngine(t *testing.T) {
	p := newCmdProject(t)
	// A sign-in lives in the global data config, where `crush login` and
	// the logout's config.ScopeGlobal write.
	expires := time.Now().Add(24 * time.Hour).Unix()
	signIn, err := json.Marshal(map[string]any{
		"providers": map[string]any{
			"hyper": map[string]any{
				"api_key": "test-key",
				"oauth": map[string]any{
					"access_token": "test-access", "refresh_token": "test-refresh",
					"expires_in": 86400, "expires_at": expires,
				},
			},
		},
	})
	require.NoError(t, err)
	signInFile := filepath.Join(p.globalData, "crush.json")
	require.NoError(t, os.WriteFile(signInFile, signIn, 0o600))
	ctx := t.Context()

	h := p.ensure(t, p.cwd)
	desktop, err := client.NewClient(p.cwd, "unix", h.Socket)
	require.NoError(t, err)
	ws, err := desktop.CreateWorkspace(ctx, prototest.CreateRequest(proto.Workspace{Path: p.cwd}))
	require.NoError(t, err)
	t.Cleanup(func() { _ = desktop.RetireClient(context.Background()) })
	require.ErrorIs(t, db.ProbeProjectLock(p.dataDir), db.ErrDataDirLocked, "the engine holds the project")
	cfg, err := desktop.GetConfig(ctx, ws.ID)
	require.NoError(t, err)
	hyper, ok := cfg.Providers.Get("hyper")
	require.True(t, ok)
	require.NotNil(t, hyper.OAuthToken, "signed in before the logout")

	stdout, stderr, code := runCrush(t, p.cwd, os.Environ(), "logout", "--force", "hyper")
	require.Equal(t, 0, code, "stdout: %s\nstderr: %s", stdout, stderr)
	require.Contains(t, stdout, "Successfully logged out of Charm Hyper.")

	cfg, err = desktop.GetConfig(ctx, ws.ID)
	require.NoError(t, err)
	hyper, _ = cfg.Providers.Get("hyper")
	require.Nil(t, hyper.OAuthToken, "the engine's loaded config forgot the sign-in")
	data, err := os.ReadFile(signInFile)
	require.NoError(t, err)
	var saved struct {
		Providers map[string]map[string]json.RawMessage `json:"providers"`
	}
	require.NoError(t, json.Unmarshal(data, &saved))
	require.NotContains(t, saved.Providers["hyper"], "oauth")

	require.NoFileExists(t, defaultSocket(t), "no legacy server socket")
	info, err := desktop.EngineInfo(ctx)
	require.NoError(t, err)
	require.Equal(t, h.PID, info.PID)
	require.Equal(t, ws.ID, info.WorkspaceID, "the engine's workspace is still there")
}

// TestSetupLocalAppTakesNoLockBeforePhase7: until the phase-7 setting
// exists, a terminal window that works alone takes no project lock, so it
// opens a project while an engine holds it.
func TestSetupLocalAppTakesNoLockBeforePhase7(t *testing.T) {
	p := newCmdProject(t)
	p.ensure(t, p.cwd)
	require.ErrorIs(t, db.ProbeProjectLock(p.dataDir), db.ErrDataDirLocked, "the engine holds the project")
	t.Chdir(p.cwd)

	app, cleanup, err := setupLocalApp(setupCmd(t))
	require.NoError(t, err, "the lone window opens the project")
	t.Cleanup(cleanup)
	dataDir, err := filepath.EvalSymlinks(app.Store().Config().Options.DataDirectory)
	require.NoError(t, err)
	require.Equal(t, p.dataDir, dataDir, "it opened the engine's project")
}

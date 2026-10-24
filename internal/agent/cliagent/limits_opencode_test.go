package cliagent

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// OpenCode Go's usage comes from its usage address with the active Console
// sign-in OpenCode itself uses (owner, 2026-10-23): the 5-hour, weekly and
// monthly windows. The old opencode-go key in auth.json is a different
// account and must not be used.
func TestOpenCodeGoUsageIsRead(t *testing.T) {
	data := t.TempDir()
	t.Setenv("XDG_DATA_HOME", data)
	dir := filepath.Join(data, "opencode")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	writeOpenCodeCredential(t, filepath.Join(dir, "opencode.db"), `{"type":"oauth","access":"console-token"}`)
	// A stale key from another account: a reading must never use it.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "auth.json"), []byte(`{"opencode-go":{"type":"api","key":"go-key"}}`), 0o600))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer console-token" {
			http.Error(w, "wrong sign-in", http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"usage":{"rolling":{"status":"ok","percent":3,"resetsAt":"2026-10-24T04:42:22.000Z"},
			"weekly":{"status":"ok","percent":1,"resetsAt":"2026-10-26T00:00:00.000Z"},
			"monthly":{"status":"ok","percent":0,"resetsAt":"2026-11-23T23:29:52.000Z"}}}`))
	}))
	defer server.Close()
	restore := openCodeUsageURL
	openCodeUsageURL = server.URL
	t.Cleanup(func() { openCodeUsageURL = restore })

	limits, err := fetchOpenCodeLimits(context.Background())
	require.NoError(t, err)
	require.Equal(t, []Limit{
		{Name: "5h", Used: 3, ResetsAt: time.Date(2026, 10, 24, 4, 42, 22, 0, time.UTC)},
		{Name: "Weekly", Used: 1, ResetsAt: time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)},
		{Name: "Monthly", Used: 0, ResetsAt: time.Date(2026, 11, 23, 23, 29, 52, 0, time.UTC)},
	}, limits)
}

// Without an active Console sign-in there is no reading: the stale
// opencode-go key in auth.json is not a fallback (owner, 2026-10-23).
func TestOpenCodeGoNeedsConsoleSignIn(t *testing.T) {
	data := t.TempDir()
	t.Setenv("XDG_DATA_HOME", data)
	dir := filepath.Join(data, "opencode")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "auth.json"), []byte(`{"opencode-go":{"type":"api","key":"go-key"}}`), 0o600))

	_, err := fetchOpenCodeLimits(context.Background())
	require.ErrorContains(t, err, "OpenCode Console is not signed in")
}

func TestOpenCodeGoLimitsRefreshDuringToolSteps(t *testing.T) {
	fetched := make(chan struct{}, 1)
	restore := SetLimitFetchersForTest(map[catwalk.Type]func(context.Context) ([]Limit, error){
		config.TypeOpenCodeCLI: func(context.Context) ([]Limit, error) {
			select {
			case fetched <- struct{}{}:
			default:
			}
			return []Limit{{Name: "5h", Used: 42}}, nil
		},
	})
	defer restore()
	r := &acpRun{
		ctx:   context.Background(),
		m:     &Model{Kind: config.TypeOpenCodeCLI},
		t:     Turn{Emit: func(Event) error { return nil }},
		name:  "opencode",
		calls: map[string]*acpTool{},
	}
	msg := rpcMessage{Params: json.RawMessage(`{"update":{"sessionUpdate":"tool_call","toolCallId":"call-1","kind":"read","status":"in_progress","rawInput":{"path":"README.md"}}}`)}
	require.NoError(t, r.handleUpdate(msg))
	select {
	case <-fetched:
	case <-time.After(time.Second):
		t.Fatal("OpenCode tool step did not refresh usage limits")
	}
	require.Eventually(t, func() bool {
		limits := Limits(config.TypeOpenCodeCLI, "")
		return len(limits) == 1 && limits[0].Used == 42
	}, time.Second, 10*time.Millisecond)
}

// writeOpenCodeCredential makes the credential table OpenCode v2 keeps in
// opencode.db, with one active Console sign-in.
func writeOpenCodeCredential(t *testing.T, path, value string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE credential (
		id text PRIMARY KEY,
		integration_id text,
		label text NOT NULL,
		value text NOT NULL,
		active integer
	)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO credential (id, integration_id, label, value, active)
		VALUES ('cred-1', 'opencode', 'Personal', ?, 1)`, value)
	require.NoError(t, err)
	require.NoError(t, db.Close())
}

// Zen pays per model from credits, so it reports no usage windows: no
// fetcher is registered for it and the meter stays hidden (owner, 2026-10-22).
func TestOpenCodeZenReportsNoUsage(t *testing.T) {
	_, err := RefreshAccountLimits(t.Context(), config.TypeOpenCodeZenCLI, "", true)
	require.ErrorIs(t, err, ErrNoUsageSource)
}

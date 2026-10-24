package cliagent

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// claudeFakeHome gives the account an expired login and a `claude` script
// that writes a fresh one, so the usage read can run without a real login.
func claudeFakeHome(t *testing.T, script string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".claude")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".credentials.json"), []byte(`{"claudeAiOauth":{"accessToken":"old","expiresAt":1}}`), 0o600))
	bin := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(bin, "claude"), []byte("#!/bin/sh\n"+script), 0o755))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// TestClaudeExpiredLoginIsRefreshedBeforeUsage: an expired account is looked
// at again through Claude Code's own status command, which renews the login
// without a browser or a model request, then the usage address is read.
func TestClaudeExpiredLoginIsRefreshedBeforeUsage(t *testing.T) {
	claudeFakeHome(t, `cat > "$HOME/.claude/.credentials.json" <<'EOF'
{"claudeAiOauth":{"accessToken":"fresh","expiresAt":4102444800000}}
EOF
`)
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"five_hour":{"utilization":12,"resets_at":"2026-10-24T18:00:00Z"}}`))
	}))
	t.Cleanup(server.Close)
	saved := claudeUsageURL
	claudeUsageURL = server.URL
	t.Cleanup(func() { claudeUsageURL = saved })

	limits, err := fetchClaudeLimits(t.Context())
	require.NoError(t, err)
	require.Equal(t, "Bearer fresh", gotAuth)
	require.Len(t, limits, 1)
	require.Equal(t, "5h", limits[0].Name)
	require.InDelta(t, 12, limits[0].Used, 0.001)
}

func TestClaudeExpiredLoginStaysUnreadWhenRefreshFails(t *testing.T) {
	claudeFakeHome(t, "exit 1\n")
	hits := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits++ }))
	t.Cleanup(server.Close)
	saved := claudeUsageURL
	claudeUsageURL = server.URL
	t.Cleanup(func() { claudeUsageURL = saved })

	_, err := fetchClaudeLimits(t.Context())
	require.ErrorContains(t, err, "claude login expired")
	require.Zero(t, hits)
}

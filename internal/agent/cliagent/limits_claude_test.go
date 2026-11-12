package cliagent

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"charm.land/catwalk/v3/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/config"
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
	require.Error(t, err)
	require.Zero(t, hits)
}

// Anthropic's usage answer lists a model family's own limit apart from the
// account's (a weekly one for Fable; owner, 2026-11-12): it shows on that
// family's chats only, and a turn's own update, which carries only the
// account's windows, does not take it away.
func TestClaudeModelLimitShowsOnItsModelAndOutlastsATurn(t *testing.T) {
	claudeFakeHome(t, "exit 1\n")
	home, _ := os.UserHomeDir()
	require.NoError(t, os.WriteFile(filepath.Join(home, ".claude", ".credentials.json"), []byte(`{"claudeAiOauth":{"accessToken":"fresh","expiresAt":4102444800000}}`), 0o600))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"five_hour":{"utilization":12,"resets_at":"2099-01-01T00:00:00Z"},"seven_day":{"utilization":39,"resets_at":"2099-01-01T00:00:00Z"},"limits":[
{"kind":"session","group":"session","percent":12,"resets_at":"2099-01-01T00:00:00Z","scope":null},
{"kind":"weekly_all","group":"weekly","percent":39,"resets_at":"2099-01-01T00:00:00Z","scope":null},
{"kind":"weekly_scoped","group":"weekly","percent":43,"resets_at":"2099-01-01T00:00:00Z","scope":{"model":{"id":null,"display_name":"Fable"},"surface":null}}]}`))
	}))
	t.Cleanup(server.Close)
	saved := claudeUsageURL
	claudeUsageURL = server.URL
	t.Cleanup(func() { claudeUsageURL = saved })

	limits, err := fetchClaudeLimits(t.Context())
	require.NoError(t, err)
	key := limitKey{config.TypeClaudeCode, "fable-test"}
	setLimits(key, limits, -1)
	names := func(model string) []string {
		var out []string
		for _, l := range AccountLimits(key.kind, key.account, model) {
			out = append(out, fmt.Sprintf("%s %.0f", l.Name, l.Used))
		}
		return out
	}
	require.Equal(t, []string{"5h 12", "Weekly 39", "Weekly Fable 43"}, names("fable"))
	require.Equal(t, []string{"5h 12", "Weekly 39"}, names("opus"))

	turn := claudeRateLimit([]byte(`{"type":"rate_limit_event","rate_limit_info":{"unifiedWindows":{"five_hour":{"utilization":0.2,"resetsAt":4102444800},"seven_day":{"utilization":0.4,"resetsAt":4102444800}}}}`))
	setLimits(key, withModelLimits(key, turn), -1)
	require.Equal(t, []string{"5h 20", "Weekly 40", "Weekly Fable 43"}, names("fable"))
}

// A working Claude chat asks for its whole usage reading itself, since the
// apps' timers wait while it works, but only once every few minutes.
func TestClaudeTurnAsksForUsageOnlyOnceInAWhile(t *testing.T) {
	var asked atomic.Int32
	defer SetLimitFetchersForTest(map[catwalk.Type]func(context.Context) ([]Limit, error){
		config.TypeClaudeCode: func(context.Context) ([]Limit, error) {
			asked.Add(1)
			return []Limit{{Name: "Weekly Fable", Used: 43, Model: "Fable"}}, nil
		},
	})()
	key := limitKey{config.TypeClaudeCode, "turn-usage-test"}
	refreshUsageDuringTurn(t.Context(), key)
	require.Eventually(t, func() bool { return len(AccountLimits(key.kind, key.account, "fable")) == 1 }, time.Second, 5*time.Millisecond)
	refreshUsageDuringTurn(t.Context(), key)
	require.Never(t, func() bool { return asked.Load() > 1 }, 200*time.Millisecond, 20*time.Millisecond)
}

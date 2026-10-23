package cliagent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/stretchr/testify/require"
)

// OpenCode Go's usage comes from its usage address with OpenCode's own
// saved Go key (owner, 2026-10-22): the 5-hour, weekly and monthly windows.
func TestOpenCodeGoUsageIsRead(t *testing.T) {
	data := t.TempDir()
	t.Setenv("XDG_DATA_HOME", data)
	require.NoError(t, os.MkdirAll(filepath.Join(data, "opencode"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(data, "opencode", "auth.json"), []byte(`{"opencode-go":{"type":"api","key":"go-key"}}`), 0o600))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer go-key", r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{"usage":{"rolling":{"status":"ok","percent":12,"resetsAt":"2026-10-22T14:01:22.291Z"},
			"weekly":{"status":"rate-limited","percent":100,"resetsAt":"2026-10-26T00:00:00.000Z"},
			"monthly":{"status":"ok","percent":78,"resetsAt":"2026-11-10T00:25:27.000Z"}}}`))
	}))
	defer server.Close()
	restore := openCodeUsageURL
	openCodeUsageURL = server.URL
	t.Cleanup(func() { openCodeUsageURL = restore })

	limits, err := fetchOpenCodeLimits(context.Background())
	require.NoError(t, err)
	require.Equal(t, []Limit{
		{Name: "5h", Used: 12, ResetsAt: time.Date(2026, 10, 22, 14, 1, 22, 291_000_000, time.UTC)},
		{Name: "Weekly", Used: 100, ResetsAt: time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)},
		{Name: "Monthly", Used: 78, ResetsAt: time.Date(2026, 11, 10, 0, 25, 27, 0, time.UTC)},
	}, limits)

	require.NoError(t, os.Remove(filepath.Join(data, "opencode", "auth.json")))
	_, err = fetchOpenCodeLimits(context.Background())
	require.Error(t, err, "no saved Go key, no reading")
}

// Zen pays per model from credits, so it reports no usage windows: no
// fetcher is registered for it and the meter stays hidden (owner, 2026-10-22).
func TestOpenCodeZenReportsNoUsage(t *testing.T) {
	_, err := RefreshAccountLimits(t.Context(), config.TypeOpenCodeZenCLI, "", true)
	require.ErrorIs(t, err, ErrNoUsageSource)
}

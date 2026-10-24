package cliagent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestFetchKimiLimits(t *testing.T) {

	reset := time.Date(2026, 10, 24, 18, 30, 0, 0, time.UTC)
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"usages": map[string]any{
				"limit_5h":          map[string]any{"used_ratio": 0.4, "reset_time": reset.Format(time.RFC3339)},
				"limit_7d":          map[string]any{"used_ratio": 0.9},
				"limit_month_total": map[string]any{"used_ratio": 0.1},
			},
		})
	}))
	defer srv.Close()

	restoreURL := kimiUsageURL
	kimiUsageURL = srv.URL
	defer func() { kimiUsageURL = restoreURL }()

	restoreKeys := ReplaceLimitProviderKeySourceForTest(func(id string) string {
		require.Equal(t, kimiProvider, id)
		return "sk-kimi"
	})
	defer restoreKeys()

	limits, err := fetchKimiLimits(context.Background())
	require.NoError(t, err)
	require.Equal(t, "Bearer sk-kimi", gotAuth)
	require.Len(t, limits, 2, "weekly usage is gone from the new plans and must be ignored")
	require.Equal(t, "5h", limits[0].Name)
	require.InDelta(t, 40, limits[0].Used, 1e-9)
	require.True(t, limits[0].ResetsAt.Equal(reset))
	require.Equal(t, "Monthly", limits[1].Name)
	require.InDelta(t, 10, limits[1].Used, 1e-9)
	require.True(t, limits[1].ResetsAt.IsZero())
}

func TestFetchKimiLimitsWithoutMonthly(t *testing.T) {

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"usages": map[string]any{
				"limit_5h": map[string]any{"used_ratio": 0.25},
			},
		})
	}))
	defer srv.Close()

	restoreURL := kimiUsageURL
	kimiUsageURL = srv.URL
	defer func() { kimiUsageURL = restoreURL }()

	restoreKeys := ReplaceLimitProviderKeySourceForTest(func(string) string { return "sk-kimi" })
	defer restoreKeys()

	limits, err := fetchKimiLimits(context.Background())
	require.NoError(t, err)
	require.Len(t, limits, 1)
	require.Equal(t, "5h", limits[0].Name)
}

func TestFetchKimiLimitsRefused(t *testing.T) {

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	restoreURL := kimiUsageURL
	kimiUsageURL = srv.URL
	defer func() { kimiUsageURL = restoreURL }()

	restoreKeys := ReplaceLimitProviderKeySourceForTest(func(string) string { return "sk-kimi" })
	defer restoreKeys()

	_, err := fetchKimiLimits(context.Background())
	require.Error(t, err)
}

func TestFetchKimiLimitsWithoutKey(t *testing.T) {

	restoreKeys := ReplaceLimitProviderKeySourceForTest(func(string) string { return "" })
	defer restoreKeys()

	_, err := fetchKimiLimits(context.Background())
	require.Error(t, err)
}

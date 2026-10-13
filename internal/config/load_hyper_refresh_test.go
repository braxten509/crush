package config

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"sync/atomic"
	"testing"
	"time"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/stretchr/testify/require"
)

const hyperRefreshChildEnv = "CRUSH_TEST_HYPER_REFRESH_DURING_LOAD"

// TestLoad_HyperTokenRefreshDuringCatalogFetch is a regression test for a
// startup deadlock. When the Hyper catalog fetch got a 401, the token
// refresher persisted the new token, which auto-reloaded the config, and
// the reload re-entered the provider catalog fetch that was still running
// and waiting on the refresher.
//
// The scenario runs in a child process: hyper.BaseURL is memoized per
// process, and a deadlock must not hang the whole package.
func TestLoad_HyperTokenRefreshDuringCatalogFetch(t *testing.T) {
	if os.Getenv(hyperRefreshChildEnv) == "1" {
		runHyperRefreshDuringLoad(t)
		return
	}

	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestLoad_HyperTokenRefreshDuringCatalogFetch$", "-test.v")
	cmd.Env = append(os.Environ(), hyperRefreshChildEnv+"=1")
	out, err := cmd.CombinedOutput()
	require.NoError(t, ctx.Err(), "Load deadlocked refreshing the Hyper token:\n%s", out)
	require.NoError(t, err, "%s", out)
}

func runHyperRefreshDuringLoad(t *testing.T) {
	var providerCalls, exchanges atomic.Int32
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/provider":
			if providerCalls.Add(1) == 1 || r.Header.Get("Authorization") != "Bearer fresh" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(catwalk.Provider{
				ID:          "hyper",
				Name:        "Charm Hyper",
				APIEndpoint: srv.URL + "/v1",
				Models:      []catwalk.Model{{ID: "hyper-model", Name: "Hyper Model"}},
			})
		case "/token/exchange":
			exchanges.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "fresh",
				"refresh_token": "rotated",
				"expires_in":    3600,
			})
		default:
			// Catwalk is unavailable; the embedded catalog is used.
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	e := newPipelineEnv(t)
	t.Setenv("HYPER_URL", srv.URL)
	t.Setenv("CATWALK_URL", srv.URL)
	// Let the catalog fetch run, so Hyper is asked and answers 401.
	t.Setenv("CRUSH_DISABLE_PROVIDER_AUTO_UPDATE", "")
	e.write(t, e.globalData, `{
		"providers": {
			"hyper": {
				"api_key": "stale",
				"oauth": {"access_token": "stale", "refresh_token": "refresh", "expires_at": 1}
			}
		}
	}`)

	store, err := Load(e.workDir, e.dataDir, false)
	require.NoError(t, err)
	require.EqualValues(t, 1, exchanges.Load())

	hyperProvider, ok := store.Config().Providers.Get("hyper")
	require.True(t, ok)
	require.Equal(t, "fresh", hyperProvider.OAuthToken.AccessToken)
	require.Equal(t, "hyper-model", hyperProvider.Models[0].ID)
}

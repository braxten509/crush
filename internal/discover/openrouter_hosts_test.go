package discover

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Hosts go cheapest first by input plus output price; equal ones go by
// cache-read price (owner, 2026-10-22), with time-of-day prices applied.
func TestOpenRouterHostsGoCheapestFirstWithCacheAsTieBreak(t *testing.T) {
	body := []byte(`{"data":{"endpoints":[
	 {"tag":"pricey","pricing":{"prompt":"0.0000003","completion":"0.0000012","input_cache_read":"0.000000006"}},
	 {"tag":"cheap/fp8","pricing":{"prompt":"0.00000007","completion":"0.0000005","input_cache_read":"0.000000008"}},
	 {"tag":"cheap-cache","pricing":{"prompt":"0.00000007","completion":"0.0000005","input_cache_read":"0.000000002"}},
	 {"tag":"night","pricing":{"prompt":"0.0000003","completion":"0.0000012","input_cache_read":"0.000000006",
	   "overrides":[{"utc_start":0,"utc_end":100,"utc_days":["monday"],"prompt":"0.00000001","completion":"0.00000001","input_cache_read":"0"}]}}
	]}}`)
	monday := func(h, m int) time.Time { return time.Date(2026, 10, 19, h, m, 0, 0, time.UTC) }
	require.Equal(t, []string{"cheap-cache", "cheap/fp8", "pricey", "night"}, openRouterHostOrder(body, monday(12, 0)))
	require.Equal(t, []string{"night", "cheap-cache", "cheap/fp8", "pricey"}, openRouterHostOrder(body, monday(0, 30)))
	require.Equal(t, []string{"cheap-cache", "cheap/fp8", "pricey", "night"}, openRouterHostOrder(body, monday(0, 30).AddDate(0, 0, 1)), "the override is for Mondays only")
}

// The order is read in the background and kept: the first ask has none,
// later asks get it without waiting.
func TestOpenRouterHostOrderIsReadInTheBackground(t *testing.T) {
	hits := make(chan string, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits <- r.URL.Path
		_, _ = w.Write([]byte(`{"data":{"endpoints":[{"tag":"a","pricing":{"prompt":"2","completion":"0"}},{"tag":"b","pricing":{"prompt":"1","completion":"0"}}]}}`))
	}))
	defer server.Close()
	hosts := newOpenRouterHosts(time.Hour)
	require.Nil(t, hosts.Order(context.Background(), server.URL+"/api/v1", "deepseek/v4"))
	require.Equal(t, "/api/v1/models/deepseek/v4/endpoints", <-hits)
	require.Eventually(t, func() bool {
		return len(hosts.Order(context.Background(), server.URL+"/api/v1", "deepseek/v4")) == 2
	}, 5*time.Second, 10*time.Millisecond)
	require.Equal(t, []string{"b", "a"}, hosts.Order(context.Background(), server.URL+"/api/v1", "deepseek/v4"))
	require.Len(t, hits, 0, "a kept order is not read again within the hour")
}

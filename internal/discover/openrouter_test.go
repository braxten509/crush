package discover

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/stretchr/testify/require"
)

type plainResolver struct{}

func (plainResolver) ResolveValue(v string) (string, error) { return v, nil }

func TestOpenRouterCatalogFollowsAccountPreferences(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
		// The account's list: its provider and privacy preferences applied.
		require.Equal(t, "/api/v1/models/user", r.URL.Path)
		_, _ = w.Write([]byte(`{"data":[
		 {"id":"moonshot/kimi-k3","name":"Moonshot: Kimi K3","context_length":262144,
		  "architecture":{"input_modalities":["text","image"]},
		  "pricing":{"prompt":"0.0000006","completion":"0.0000025","input_cache_read":"0.00000015"},
		  "top_provider":{"max_completion_tokens":65536},
		  "supported_parameters":["tools","reasoning","max_tokens"]},
		 {"id":"plain/chat","name":"Plain Chat","context_length":8192,
		  "architecture":{"input_modalities":["text"]},
		  "pricing":{"prompt":"0","completion":"0"},
		  "supported_parameters":["max_tokens"]},
		 {"id":"free/coder:free","name":"Free Coder","context_length":131072,
		  "architecture":{"input_modalities":["text"]},
		  "pricing":{"prompt":"0","completion":"0"},
		  "top_provider":{"max_completion_tokens":0},
		  "supported_parameters":["tools"]},
		 {"id":"openrouter/auto","name":"Auto Router","context_length":2000000,
		  "pricing":{"prompt":"-1","completion":"-1"},
		  "supported_parameters":["tools"]}
		]}`))
	}))
	defer server.Close()

	models, err := OpenRouterCatalog(context.Background(), server.URL+"/api/v1", "test-key", nil, plainResolver{})
	require.NoError(t, err)

	ids := make([]string, len(models))
	for i, m := range models {
		ids[i] = m.ID
	}
	// Agents need tools, so a model without them is left out.
	require.Equal(t, []string{"moonshot/kimi-k3", "free/coder:free", "openrouter/auto"}, ids)

	kimi := models[0]
	require.Equal(t, "Moonshot: Kimi K3", kimi.Name)
	require.EqualValues(t, 262144, kimi.ContextWindow)
	require.EqualValues(t, 32000, kimi.DefaultMaxTokens)
	// It reasons, but lists no effort choice: no levels.
	require.True(t, kimi.CanReason)
	require.Empty(t, kimi.ReasoningLevels)
	require.Empty(t, kimi.DefaultReasoningEffort)
	require.True(t, kimi.SupportsImages)
	require.InDelta(t, 0.6, kimi.CostPer1MIn, 1e-9)
	require.InDelta(t, 2.5, kimi.CostPer1MOut, 1e-9)
	require.InDelta(t, 0.15, kimi.CostPer1MInCached, 1e-9)

	free := models[1]
	require.False(t, free.CanReason)
	require.Empty(t, free.ReasoningLevels)
	require.False(t, free.SupportsImages)
	require.EqualValues(t, 32000, free.DefaultMaxTokens)
	require.Zero(t, free.CostPer1MIn)

	// A router's "-1" means the price varies, shown as no price.
	require.Equal(t, catwalk.Model{}.CostPer1MIn, models[2].CostPer1MIn)
}

// Each model gets the effort levels and default OpenRouter lists for it
// (owner, 2026-10-22), lowest first, without "none".
func TestOpenRouterModelsTakeTheirOwnEffortLevels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[
		 {"id":"deepseek/deepseek-v4.1-flash-1017","supported_parameters":["tools","reasoning"],
		  "reasoning":{"mandatory":false,"supported_efforts":["max","high","low","none"],"default_effort":"high"}},
		 {"id":"any/effort","supported_parameters":["tools","reasoning"],
		  "reasoning":{"supported_efforts":null,"default_effort":"medium"}},
		 {"id":"off/by-default","supported_parameters":["tools","reasoning"],
		  "reasoning":{"supported_efforts":["high","medium","low","none"],"default_effort":"none"}}
		]}`))
	}))
	defer server.Close()

	models, err := OpenRouterCatalog(context.Background(), server.URL, "test-key", nil, plainResolver{})
	require.NoError(t, err)
	require.Len(t, models, 3)
	got := func(m catwalk.Model) []any { return []any{m.CanReason, m.ReasoningLevels, m.DefaultReasoningEffort} }
	require.Equal(t, []any{true, []string{"low", "high", "max"}, "high"}, got(models[0]))
	// No list means every level OpenRouter has.
	require.Equal(t, []any{true, []string{"minimal", "low", "medium", "high", "xhigh", "max"}, "medium"}, got(models[1]))
	// Off by default: the lowest level.
	require.Equal(t, []any{true, []string{"low", "medium", "high"}, "low"}, got(models[2]))
}

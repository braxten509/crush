package discover

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

// OrcaRouter's own /models list fills the "+ Add model" sheet (owner,
// 2026-10-27): only models that call tools, its own auto-pick routers left
// out, names without the vendor word, and Low, Medium, High for every
// model that thinks.
func TestOrcaRouterCatalog(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/models", r.URL.Path)
		_, _ = w.Write([]byte(`{"data":[
		 {"id":"orcarouter/fusion","owned_by":"orcarouter","context_length":1000000},
		 {"id":"anthropic/claude-fable-5","name":"Anthropic: Claude Fable 5","context_length":1000000,
		  "max_completion_tokens":128000,
		  "architecture":{"input_modalities":["text","image","file"]},
		  "pricing":{"prompt":"0.0000100000","completion":"0.0000500000","input_cache_read":"0.0000010000"},
		  "supported_parameters":["max_tokens","reasoning","tools"]},
		 {"id":"z-ai/glm-5.3","name":"Z.ai: GLM 5.3","context_length":200000,
		  "architecture":{"input_modalities":["text"]},
		  "pricing":{"prompt":"0.0000005","completion":"0.0000019"},
		  "top_provider":{"max_completion_tokens":16000},
		  "supported_parameters":["tools","reasoning_effort"]},
		 {"id":"plain/coder","name":"Plain Coder","context_length":131072,
		  "pricing":{"prompt":"0","completion":"0"},
		  "supported_parameters":["tools"]},
		 {"id":"openai/gpt-image-2","name":"OpenAI: GPT Image 2","supported_parameters":["size"]}
		]}`))
	}))
	defer server.Close()

	models, err := OrcaRouterCatalog(context.Background(), server.URL+"/v1", "", nil, plainResolver{})
	require.NoError(t, err)

	ids := make([]string, len(models))
	for i, m := range models {
		ids[i] = m.ID
	}
	require.Equal(t, []string{"anthropic/claude-fable-5", "z-ai/glm-5.3", "plain/coder"}, ids)

	fable := models[0]
	require.Equal(t, "Claude Fable 5", fable.Name)
	require.EqualValues(t, 1000000, fable.ContextWindow)
	require.EqualValues(t, 32000, fable.DefaultMaxTokens)
	require.True(t, fable.SupportsImages)
	require.InDelta(t, 10, fable.CostPer1MIn, 1e-9)
	require.InDelta(t, 50, fable.CostPer1MOut, 1e-9)
	require.InDelta(t, 1, fable.CostPer1MInCached, 1e-9)
	require.True(t, fable.CanReason)
	require.Equal(t, []string{"low", "medium", "high"}, fable.ReasoningLevels)
	require.Equal(t, "medium", fable.DefaultReasoningEffort)

	glm := models[1]
	require.True(t, glm.CanReason, "reasoning_effort alone counts as thinking")
	require.Equal(t, []string{"low", "medium", "high"}, glm.ReasoningLevels)
	require.EqualValues(t, 16000, glm.DefaultMaxTokens)

	plain := models[2]
	require.False(t, plain.CanReason)
	require.Empty(t, plain.ReasoningLevels)
	require.Zero(t, plain.CostPer1MIn)
}

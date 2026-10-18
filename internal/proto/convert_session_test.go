package proto

import (
	"encoding/json"
	"testing"

	"github.com/charmbracelet/crush/internal/session"
	"github.com/stretchr/testify/require"
)

func TestSessionWireKeepsContextBudget(t *testing.T) {
	t.Parallel()

	in := session.Session{
		ID:             "s1",
		Title:          "chat",
		EstimatedUsage: true,
		ContextBudget: session.ContextBudget{
			Provider:  "anthropic",
			Model:     "claude",
			Tokens:    150000,
			Estimated: true,
		},
	}
	wire := SessionFromDomain(in)
	require.Equal(t, &ContextBudget{Provider: "anthropic", Model: "claude", Tokens: 150000, Estimated: true}, wire.ContextBudget)
	require.True(t, wire.EstimatedUsage)

	// Through JSON, as the server sends it and an app sends it back.
	data, err := json.Marshal(wire)
	require.NoError(t, err)
	require.Contains(t, string(data), `"context_budget":{"provider":"anthropic","model":"claude","tokens":150000,"estimated":true}`)
	require.Contains(t, string(data), `"estimated_usage":true`)
	var decoded Session
	require.NoError(t, json.Unmarshal(data, &decoded))
	out := SessionToDomain(decoded)
	require.Equal(t, in.ContextBudget, out.ContextBudget)
	require.True(t, out.EstimatedUsage)

	// No budget: nil on the wire, the zero budget back.
	none := SessionFromDomain(session.Session{ID: "s2"})
	require.Nil(t, none.ContextBudget)
	data, err = json.Marshal(none)
	require.NoError(t, err)
	require.NotContains(t, string(data), "context_budget")
	require.Contains(t, string(data), `"estimated_usage":false`)
	require.Contains(t, string(data), `"has_unfinished_work":false`)
	require.Contains(t, string(data), `"pending_prompt":""`)
	require.Equal(t, session.ContextBudget{}, SessionToDomain(none).ContextBudget)
	require.False(t, SessionToDomain(none).EstimatedUsage)
}

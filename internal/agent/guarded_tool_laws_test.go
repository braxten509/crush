package agent

import (
	"context"
	"testing"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/secretguard"
	"github.com/stretchr/testify/require"
)

type stubGuardedTool struct {
	name string
	ran  bool
}

func (s *stubGuardedTool) Info() fantasy.ToolInfo {
	return fantasy.ToolInfo{Name: s.name}
}

func (s *stubGuardedTool) ProviderOptions() fantasy.ProviderOptions { return nil }

func (s *stubGuardedTool) SetProviderOptions(fantasy.ProviderOptions) {}

func (s *stubGuardedTool) Run(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error) {
	s.ran = true
	return fantasy.NewTextResponse("ok"), nil
}

func TestGuardedToolBlocksLawsWithWorkDir(t *testing.T) {
	t.Parallel()

	inner := &stubGuardedTool{name: tools.BashToolName}
	g := guardedTool{inner}
	ctx := withGuardedWorkDir(t.Context(), "/repo/tools/laws")
	resp, err := g.Run(ctx, fantasy.ToolCall{
		Name:  tools.BashToolName,
		Input: `{"command":"./approve"}`,
	})
	require.NoError(t, err)
	require.True(t, resp.IsError)
	require.Equal(t, secretguard.LawsReason, resp.Content)
	require.False(t, inner.ran)

	resp, err = g.Run(ctx, fantasy.ToolCall{
		Name:  tools.BashToolName,
		Input: `{"command":"./check"}`,
	})
	require.NoError(t, err)
	require.False(t, resp.IsError)
	require.True(t, inner.ran)
}

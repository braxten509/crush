package agent

import (
	"testing"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/agent/tools/mcp"
	"github.com/charmbracelet/crush/internal/db"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/stretchr/testify/require"
)

func TestMCPFiltersPreserveSafetyWrappers(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	t.Cleanup(func() { require.NoError(t, db.Release(dataDir)) })
	conn, err := db.Connect(t.Context(), dataDir)
	require.NoError(t, err)
	sessions := session.NewService(db.New(conn), conn)
	agent := &sessionAgent{sessions: sessions}
	require.NoError(t, sessions.SetMCPServerDisabled(t.Context(), "signal", true))

	for _, wrap := range []struct {
		name string
		tool func(fantasy.AgentTool) fantasy.AgentTool
	}{
		{"unwrapped", func(tool fantasy.AgentTool) fantasy.AgentTool { return tool }},
		{"guarded", func(tool fantasy.AgentTool) fantasy.AgentTool { return guardedTool{tool} }},
		{"hooked and guarded", func(tool fantasy.AgentTool) fantasy.AgentTool {
			return newHookedTool(guardedTool{tool}, nil)
		}},
	} {
		t.Run(wrap.name, func(t *testing.T) {
			signal := wrap.tool(&channelTestTool{name: "send", mcpName: "signal"})
			other := wrap.tool(&channelTestTool{name: "search", mcpName: "search"})
			plain := wrap.tool(&fakeTool{name: "plain"})
			toolList := []fantasy.AgentTool{signal, other, plain}
			states := map[string]mcp.ClientInfo{"signal": {Channel: true}}

			require.Equal(t, toolList, filterToolsForChannel(toolList, "signal", states))
			require.Equal(t, []fantasy.AgentTool{other, plain}, filterToolsForChannel(toolList, "other", states))
			require.Equal(t, []fantasy.AgentTool{other, plain}, agent.filterDisabledMCPTools(t.Context(), toolList))
		})
	}

	require.NoError(t, sessions.SetMCPServerDisabled(t.Context(), "signal", false))
	toolList := guardTools([]fantasy.AgentTool{&channelTestTool{name: "send", mcpName: "signal"}})
	require.Equal(t, toolList, agent.filterDisabledMCPTools(t.Context(), toolList))
}

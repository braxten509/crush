package agent

import (
	"strings"
	"testing"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/csync"
	"github.com/stretchr/testify/require"
)

func TestInstructionsCarryUserSubAgentRules(t *testing.T) {
	t.Setenv("CRUSH_GLOBAL_CONFIG", t.TempDir())
	h := &taskHub{c: &coordinator{cfg: config.NewTestStore(&config.Config{Providers: csync.NewMap[string, config.ProviderConfig]()})}}
	require.NotContains(t, h.instructions(), "<user_sub_agent_instructions>", "nothing saved, nothing added")

	require.NoError(t, config.SaveSubAgentInstructions("- UI work: Claude Opus 5.5, high"))
	text := h.instructions()
	block, _, ok := cutBetween(text, "<crush_sub_agents>", "</crush_sub_agents>")
	require.True(t, ok)
	require.Contains(t, block, "<user_sub_agent_instructions>\n- UI work: Claude Opus 5.5, high\n</user_sub_agent_instructions>",
		"the saved rules close the sub-agent block")

	require.NoError(t, config.SaveSubAgentInstructions("- Reviews: read-only"))
	require.Contains(t, h.instructions(), "- Reviews: read-only", "an edit shows on the next call")
	require.NotContains(t, h.instructions(), "Opus")
}

func cutBetween(s, start, end string) (string, string, bool) {
	_, rest, ok := strings.Cut(s, start)
	if !ok {
		return "", "", false
	}
	inside, after, ok := strings.Cut(rest, end)
	return inside, after, ok
}

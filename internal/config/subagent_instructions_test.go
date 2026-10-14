package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSubAgentInstructionsSaveAndLoad(t *testing.T) {
	t.Setenv("CRUSH_GLOBAL_CONFIG", t.TempDir())
	require.Empty(t, LoadSubAgentInstructions(), "nothing saved yet")

	require.NoError(t, SaveSubAgentInstructions("  - UI work: Opus, high\n- Reviews: read-only\n\n"))
	require.Equal(t, "- UI work: Opus, high\n- Reviews: read-only", LoadSubAgentInstructions())
	data, err := os.ReadFile(SubAgentInstructionsFile())
	require.NoError(t, err)
	require.Equal(t, "- UI work: Opus, high\n- Reviews: read-only\n", string(data))

	require.NoError(t, SaveSubAgentInstructions("- Edited"))
	require.Equal(t, "- Edited", LoadSubAgentInstructions(), "saving again replaces them")
	require.NoError(t, SaveSubAgentInstructions(""))
	require.Empty(t, LoadSubAgentInstructions(), "clearing the box clears them")
}

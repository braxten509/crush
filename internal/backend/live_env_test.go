package backend

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestEnvIsSetReadsWorkspaceEnvFirst: a name is set as the workspace's `!`
// commands see it: the workspace's own Env over the engine's, the last
// entry of a name winning. Only booleans come back, and no value is logged
// (engine-design.md 5.9 b).
func TestEnvIsSetReadsWorkspaceEnvFirst(t *testing.T) {
	logs := captureDebugLogs(t)
	const engineValue, workspaceValue = "engine-secret-value", "workspace-secret-value"
	t.Setenv("CRUSH_TEST_ENGINE_ONLY", engineValue)
	t.Setenv("CRUSH_TEST_EMPTIED", engineValue)
	t.Setenv("CRUSH_TEST_ENGINE_EMPTY", "")
	b, _ := newTestBackend(t)
	ws, _ := insertTestWorkspace(t, b, t.TempDir())
	ws.Env = []string{
		"CRUSH_TEST_WORKSPACE_ONLY=" + workspaceValue,
		"CRUSH_TEST_EMPTIED=",
		"CRUSH_TEST_LAST=",
		"CRUSH_TEST_LAST=" + workspaceValue,
		"CRUSH_TEST_NOT_A_PAIR",
	}

	set, err := b.EnvIsSet(ws.ID, []string{
		"CRUSH_TEST_ENGINE_ONLY", "CRUSH_TEST_WORKSPACE_ONLY", "CRUSH_TEST_EMPTIED",
		"CRUSH_TEST_LAST", "CRUSH_TEST_ENGINE_EMPTY", "CRUSH_TEST_UNSET", "CRUSH_TEST_NOT_A_PAIR",
	})
	require.NoError(t, err)
	require.Equal(t, map[string]bool{
		"CRUSH_TEST_ENGINE_ONLY":    true,
		"CRUSH_TEST_WORKSPACE_ONLY": true,
		"CRUSH_TEST_EMPTIED":        false, // the workspace's empty value wins
		"CRUSH_TEST_LAST":           true,
		"CRUSH_TEST_ENGINE_EMPTY":   false,
		"CRUSH_TEST_UNSET":          false,
		"CRUSH_TEST_NOT_A_PAIR":     false,
	}, set)
	require.NotContains(t, logs.String(), engineValue)
	require.NotContains(t, logs.String(), workspaceValue)
}

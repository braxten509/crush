package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/crush/internal/agent"
	"github.com/stretchr/testify/require"
)

// `crush spawn --mcp` asks Crush for a sub-agent with the MCP add-ons; a
// plain spawn asks for one without them.
func TestSpawnMCPFlag(t *testing.T) {
	for _, mcp := range []bool{false, true} {
		dir := t.TempDir()
		t.Setenv(agent.TasksDirEnv, dir)
		t.Setenv(agent.TasksSessionEnv, "parent")
		require.NoError(t, spawnCmd.Flags().Set("cli", "claude"))
		if mcp {
			require.NoError(t, spawnCmd.Flags().Set("mcp", "true"))
		}
		t.Cleanup(func() {
			for _, name := range []string{"cli", "mcp"} {
				if f := spawnCmd.Flags().Lookup(name); f != nil {
					_ = f.Value.Set(f.DefValue)
					f.Changed = false
				}
			}
		})

		got := make(chan agent.TaskRequest, 1)
		go func() {
			for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
				entries, _ := os.ReadDir(dir)
				for _, e := range entries {
					if !strings.HasSuffix(e.Name(), ".req") {
						continue
					}
					data, _ := os.ReadFile(filepath.Join(dir, e.Name()))
					var req agent.TaskRequest
					_ = json.Unmarshal(data, &req)
					got <- req
					reply, _ := json.Marshal(agent.TaskReply{Task: &agent.Task{ID: "t1", CLI: "claude"}})
					_ = os.WriteFile(filepath.Join(dir, strings.TrimSuffix(e.Name(), ".req")+".ack"), reply, 0o600)
					return
				}
			}
		}()
		require.NoError(t, spawnCmd.RunE(spawnCmd, []string{"work"}))
		req := <-got
		require.Equal(t, mcp, req.MCP)
	}
}

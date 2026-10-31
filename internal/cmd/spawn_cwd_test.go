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

// `crush spawn -c FOLDER` starts the sub-agent in that folder (where an agent
// CLI may also save); a folder that isn't there is refused before asking.
func TestSpawnCwdFlag(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(agent.TasksDirEnv, dir)
	t.Setenv(agent.TasksSessionEnv, "parent")
	work := t.TempDir()
	require.NoError(t, spawnCmd.Flags().Set("cli", "codex"))
	require.NoError(t, rootCmd.PersistentFlags().Set("cwd", work))
	t.Cleanup(func() {
		if f := spawnCmd.Flags().Lookup("cli"); f != nil {
			_ = f.Value.Set(f.DefValue)
			f.Changed = false
		}
		if f := rootCmd.PersistentFlags().Lookup("cwd"); f != nil {
			_ = f.Value.Set(f.DefValue)
			f.Changed = false
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
				reply, _ := json.Marshal(agent.TaskReply{Task: &agent.Task{ID: "t1", CLI: "codex", Dir: req.Dir}})
				_ = os.WriteFile(filepath.Join(dir, strings.TrimSuffix(e.Name(), ".req")+".ack"), reply, 0o600)
				return
			}
		}
	}()
	require.NoError(t, spawnCmd.RunE(spawnCmd, []string{"work"}))
	require.Equal(t, work, (<-got).Dir)

	require.NoError(t, rootCmd.PersistentFlags().Set("cwd", filepath.Join(work, "missing")))
	require.ErrorContains(t, spawnCmd.RunE(spawnCmd, []string{"work"}), "missing")
}

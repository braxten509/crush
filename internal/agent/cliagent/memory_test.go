package cliagent

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestClaudeNativeMemoryDisabled(t *testing.T) {
	// Even inherited opt-ins must not override Crush's process-local policy.
	t.Setenv("CLAUDE_CODE_DISABLE_AUTO_MEMORY", "0")
	t.Setenv("CLAUDE_CODE_DISABLE_ORG_MEMORY", "0")
	dir := t.TempDir()
	script := "#!/bin/sh\nread -r _\nprintf '%s %s\\n' \"$CLAUDE_CODE_DISABLE_AUTO_MEMORY\" \"$CLAUDE_CODE_DISABLE_ORG_MEMORY\"\ncat >/dev/null\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	for _, noTools := range []bool{false, true} {
		live, err := startClaude(&Model{ID: "test", Dir: dir}, Turn{
			NoTools: noTools,
			Env:     []string{"CLAUDE_CODE_DISABLE_AUTO_MEMORY=0"},
		}, claudeKey{})
		require.NoError(t, err)
		func() {
			defer live.p.finish()
			select {
			case line := <-live.lines:
				require.Equal(t, "1 1", string(line))
			case <-time.After(5 * time.Second):
				t.Fatal("Claude launcher did not report its memory environment")
			}
		}()
	}
	require.Equal(t, "0", os.Getenv("CLAUDE_CODE_DISABLE_AUTO_MEMORY"))
	require.Equal(t, "0", os.Getenv("CLAUDE_CODE_DISABLE_ORG_MEMORY"))
}

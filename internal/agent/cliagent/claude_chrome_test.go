package cliagent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestClaudeHeadlessLaunchDisablesChrome(t *testing.T) {
	dir := t.TempDir()
	script := "#!/bin/sh\nread -r _\nprintf '%s\\n' \"$*\"\ncat >/dev/null\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	for _, guarded := range []bool{false, true} {
		for _, noTools := range []bool{false, true} {
			live, err := startClaude(&Model{ID: "test", Dir: dir, Guarded: guarded}, Turn{NoTools: noTools}, claudeKey{})
			require.NoError(t, err)
			func() {
				defer live.p.finish()
				select {
				case line := <-live.lines:
					require.Contains(t, strings.Fields(string(line)), "--no-chrome")
					require.NotContains(t, strings.Fields(string(line)), "--chrome")
				case <-time.After(5 * time.Second):
					t.Fatal("Claude launcher did not report its arguments")
				}
			}()
		}
	}
}

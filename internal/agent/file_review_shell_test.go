//go:build linux && amd64

package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/crush/internal/agent/cliagent"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/ui/diffreview"
	"github.com/stretchr/testify/require"
)

// Exercise the real CLI protocol, persistence and the action-group diff. The
// shell computes a filename in a sibling folder of the same Git repository.
func TestShellFileReviewCLIEndToEnd(t *testing.T) {
	for _, late := range []bool{false, true} {
		t.Run(fmt.Sprintf("late=%v", late), func(t *testing.T) {
			env := testEnv(t)
			env.workingDir = visibleReviewRoot(t)
			bin, outside := t.TempDir(), visibleReviewRoot(t)
			require.NoError(t, os.WriteFile(filepath.Join(outside, "hello.txt"), []byte("existing\n"), 0600))
			command := fmt.Sprintf(`python3 - <<'PY'
from pathlib import Path
root = Path(%q)
for index in range(1000):
 path = root / ('hello.txt' if index == 0 else f'hello-{index}.txt')
 try:
  with path.open('x') as file: file.write('hello\n')
  break
 except FileExistsError: continue
PY`, outside)
			commandJSON, _ := json.Marshal(command)
			script := fmt.Sprintf(`#!/usr/bin/python3
import json,sys,subprocess
sys.stdin.readline(); sys.stdin.readline()
def send(obj): print(json.dumps(obj),flush=True)
send({'type':'system','subtype':'init','session_id':'shell-review'})
command = %s
late = %s
if late: subprocess.run(['/bin/sh','-c',command],check=True)
send({'type':'assistant','message':{'content':[{'type':'tool_use','id':'shell','name':'Bash','input':{'command':command}}]}})
if not late: subprocess.run(['/bin/sh','-c',command],check=True)
send({'type':'user','message':{'content':[{'type':'tool_result','tool_use_id':'shell','content':'Created'}]}})
send({'type':'assistant','message':{'content':[{'type':'text','text':'Done.'}]}})
send({'type':'result','subtype':'success'})
`, commandJSON, map[bool]string{true: "True", false: "False"}[late])
			require.NoError(t, os.WriteFile(filepath.Join(bin, "claude"), []byte(script), 0755))
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			provider := cliagent.NewProvider(config.TypeClaudeCode, env.workingDir, t.TempDir(), env.permissions, env.history, "", false)
			model, err := provider.LanguageModel(t.Context(), "fixture")
			require.NoError(t, err)
			agent := testSessionAgent(env, model, &finishStreamModel{text: "fixture"}, "system").(*sessionAgent)
			agent.isSubAgent = true
			session, err := env.sessions.Create(t.Context(), "shell review")
			require.NoError(t, err)
			_, err = agent.Run(t.Context(), SessionAgentCall{SessionID: session.ID, Prompt: "Write the fixture", NonInteractive: true})
			require.NoError(t, err)
			messages, err := loadFileReviewMessages(env.messages, t.Context(), session.ID)
			require.NoError(t, err)
			var edits []diffreview.Edit
			for _, message := range messages {
				for _, result := range message.ToolResults() {
					require.NotNil(t, result.Review, "Bash's writes must carry a local review")
					require.Len(t, result.Review.Changes, 1)
					require.NotContains(t, result.Metadata, "file_review", "snapshots are not duplicated in metadata")
					for _, change := range result.Review.Changes {
						edits = append(edits, diffreview.Edit{Path: change.Path, Snapshot: &change})
					}
				}
			}
			files := diffreview.Build(edits)
			require.Len(t, files, 1)
			require.Equal(t, filepath.Join(outside, "hello-1.txt"), files[0].Path)
			require.Equal(t, diffreview.Added, files[0].Kind)
			require.Equal(t, 1, files[0].Adds)
		})
	}
}

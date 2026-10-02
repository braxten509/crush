package cliagent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/filechange"
	"github.com/stretchr/testify/require"
)

// fakeCLI puts a script named bin on PATH that reads the driver's first
// input line, prints out (recorded protocol output), then waits for the
// driver to close stdin like the real CLIs do.
func fakeCLI(t *testing.T, bin, out string) {
	dir := t.TempDir()
	script := "#!/bin/sh\nread -r _\ncat <<'EOF'\n" + out + "EOF\ncat >/dev/null\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, bin), []byte(script), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func collect(t *testing.T, kind string, resume string) ([]Event, error) {
	var events []Event
	m := &Model{Kind: map[string]catwalk.Type{
		"claude": config.TypeClaudeCode, "codex": config.TypeCodexCLI, "grok": config.TypeGrokCLI,
		"opencode": config.TypeOpenCodeCLI, "agy": config.TypeAGYCLI,
	}[kind], ID: "m", Dir: t.TempDir()}
	err := m.Run(context.Background(), Turn{Prompt: "hi", Resume: resume, Emit: func(e Event) error {
		events = append(events, e)
		return nil
	}})
	return events, err
}

func TestClaudeTurn(t *testing.T) {
	fakeCLI(t, "claude", `{"type":"system","subtype":"init","session_id":"s1"}
{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"Let me look."}}}
{"type":"stream_event","event":{"type":"content_block_start","content_block":{"type":"tool_use","id":"t1","name":"Read"}}}
{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":"Read","input":{"file_path":"/a.go","offset":3}}]}}
{"type":"control_request","request_id":"r1","request":{"subtype":"can_use_tool","tool_name":"Read","input":{"file_path":"/a.go"},"tool_use_id":"t1"}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"     3\tpackage a\n     4\tfunc A() {}"}]}}
{"type":"stream_event","event":{"type":"message_delta","usage":{"input_tokens":5,"output_tokens":7,"cache_read_input_tokens":100}}}
{"type":"stream_event","event":{"type":"message_stop"}}
{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"Done."}}}
{"type":"result","subtype":"success","result":"Done."}
`)
	events, err := collect(t, "claude", "")
	require.NoError(t, err)
	require.Equal(t, []EventType{EventSession, EventText, EventToolStart, EventToolCall, EventToolResult, EventUsage, EventText}, types(events))
	require.Equal(t, "s1", events[0].Session)
	require.Equal(t, "view", events[3].Name)
	require.JSONEq(t, `{"file_path":"/a.go","offset":2}`, events[3].Input)
	require.JSONEq(t, `{"file_path":"/a.go","content":"package a\nfunc A() {}"}`, events[4].Metadata)
	require.Equal(t, int64(100), events[5].Usage.CacheReadTokens)
}

func TestClaudeResumeFailure(t *testing.T) {
	// A bad --resume makes claude exit without starting a session.
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "claude"), []byte("#!/bin/sh\necho 'No conversation found' >&2\nexit 1\n"), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	_, err := collect(t, "claude", "gone")
	require.ErrorIs(t, err, ErrResume)
}

func TestCodexTurn(t *testing.T) {
	fakeCLI(t, "codex", `{"id":"1","result":{}}
{"id":"2","result":{"thread":{"id":"th1"}}}
{"id":"3","result":{"turn":{"id":"tu1"}}}
{"method":"item/started","params":{"item":{"type":"agentMessage","id":"m1"}}}
{"method":"item/agentMessage/delta","params":{"itemId":"m1","delta":"Fixing."}}
{"method":"item/started","params":{"item":{"type":"commandExecution","id":"c1","command":"/bin/zsh -lc \"echo 'hi'\""}}}
{"method":"item/completed","params":{"item":{"type":"commandExecution","id":"c1","status":"completed","aggregatedOutput":"hi\n","exitCode":0}}}
{"method":"item/started","params":{"item":{"type":"fileChange","id":"f1","changes":[{"path":"/x.txt","kind":{"type":"update"},"diff":"@@ -1,2 +1,2 @@\n a\n-b\n+B\n"}]}}}
{"method":"item/completed","params":{"item":{"type":"fileChange","id":"f1","status":"completed","changes":[]}}}
{"method":"thread/tokenUsage/updated","params":{"tokenUsage":{"last":{"inputTokens":50,"cachedInputTokens":40,"outputTokens":3}}}}
{"method":"turn/completed","params":{"turn":{"id":"tu1","status":"completed"}}}
`)
	events, err := collect(t, "codex", "")
	require.NoError(t, err)
	require.Equal(t, []EventType{
		EventSession, EventText,
		EventToolStart, EventToolCall, EventToolResult,
		EventToolStart, EventToolCall, EventToolResult,
		EventUsage,
	}, types(events))
	require.Equal(t, "th1", events[0].Session)
	require.JSONEq(t, `{"command":"echo 'hi'","description":""}`, events[3].Input)
	require.Equal(t, "hi\n", events[4].Output)
	require.Equal(t, "f1#0", events[6].ID)
	require.JSONEq(t, `{"file_path":"/x.txt","old_string":"a\nb\n","new_string":"a\nB\n"}`, events[6].Input)
	require.Equal(t, int64(10), events[8].Usage.InputTokens)
}

func TestUnwrapShellMixedAndNestedQuotes(t *testing.T) {
	command := "python3 - <<'PY'\nprint('hello\\n')\nPY"
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'" }
	wrapped := "/bin/zsh -lc " + quote(command)
	require.Equal(t, command, unwrapShell(wrapped))
	require.Equal(t, command, unwrapShell("/bin/zsh -lc "+quote(wrapped)))
	// Mixed segments do not have to end with the quote used at the start.
	require.Equal(t, "echo 'hello'", unwrapShell(`/bin/zsh -lc 'echo '"'hello'"`))
}

func TestHelpers(t *testing.T) {
	require.Equal(t, "echo 'a b'", unwrapShell(`/bin/zsh -lc 'echo '\''a b'\'''`))
	require.Equal(t, `say "x" $HOME`, unwrapShell(`/bin/bash -lc "say \"x\" \$HOME"`))
	require.Equal(t, "ls -la", unwrapShell("ls -la"))
	command := "python3 - <<'PY'\nprint('hello\\n')\nPY"
	require.Equal(t, command, unwrapShell("/bin/zsh -lc "+strconv.Quote(command)))
	require.Equal(t, command, unwrapShell("/bin/zsh -lc '"+strings.ReplaceAll(command, "'", `'\''`)+"'"))
	require.Equal(t, "not numbered\n  1\tx", stripLineNumbers("not numbered\n  1\tx"))

	name, input := claudeTool("mcp__github__get_issue", []byte(`{"n":1}`))
	require.Equal(t, "mcp_github_get_issue", name)
	require.Equal(t, `{"n":1}`, input)
}

func types(events []Event) []EventType {
	out := make([]EventType, len(events))
	for i, e := range events {
		out[i] = e.Type
	}
	return out
}

func TestCodexNestedShellReview(t *testing.T) {
	outside := t.TempDir()
	command := fmt.Sprintf("python3 - <<'PY'\nfrom pathlib import Path\nPath(%q).write_text('hello\\n')\nPY", filepath.Join(outside, "hello.txt"))
	commandJSON, err := json.Marshal(command)
	require.NoError(t, err)
	bin := t.TempDir()
	script := fmt.Sprintf(`#!/usr/bin/python3
import json,sys,subprocess,shlex
sys.stdin.readline()
def send(value): print(json.dumps(value),flush=True)
send({'id':'1','result':{}})
send({'id':'2','result':{'thread':{'id':'fixture'}}})
send({'id':'3','result':{'turn':{'id':'turn'}}})
command = %s
inner = shlex.join(['/bin/sh','-lc',command])
reported = shlex.join(['/bin/sh','-lc',inner])
send({'method':'item/started','params':{'item':{'type':'commandExecution','id':'write','command':reported}}})
subprocess.run(['/bin/sh','-lc',inner],check=True)
send({'method':'item/completed','params':{'item':{'type':'commandExecution','id':'write','status':'completed','aggregatedOutput':'','exitCode':0}}})
send({'method':'turn/completed','params':{'turn':{'id':'turn','status':'completed'}}})
sys.stdin.read()
`, commandJSON)
	require.NoError(t, os.WriteFile(filepath.Join(bin, "codex"), []byte(script), 0755))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	events, err := collect(t, "codex", "")
	require.NoError(t, err)
	found := false
	for _, event := range events {
		if event.Type != EventToolResult {
			continue
		}
		_, review := filechange.TakeReview(event.Metadata)
		require.NotNil(t, review, "nested shell writes must reach the diff review")
		require.Len(t, review.Changes, 1)
		require.Equal(t, filepath.Join(outside, "hello.txt"), review.Changes[0].Path)
		require.Nil(t, review.Changes[0].Before)
		require.Equal(t, "hello\n", review.Changes[0].After.Content)
		found = true
	}
	require.True(t, found)
}

package cliagent

import (
	"encoding/json"
	"testing"

	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/stretchr/testify/require"
)

// Grok gives its tools no standard kind and AGY renamed its own, so the
// Crush tool is found from the name and the input fields.
func TestOtherCLIToolNamesMapToCrushTools(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		cli, kind, name string
		input           map[string]any
		want            string
		field, value    string
	}{
		{"grok", "other", "run_terminal_command", map[string]any{"command": "make", "description": "build"}, tools.BashToolName, "command", "make"},
		{"grok", "", "read_file", map[string]any{"target_file": "/p/a.go"}, tools.ViewToolName, "file_path", "/p/a.go"},
		{"grok", "other", "search_replace", map[string]any{"file_path": "/p/a.go", "old_string": "x", "new_string": "y"}, tools.EditToolName, "new_string", "y"},
		{"grok", "other", "write", map[string]any{"file_path": "/p/b.go", "content": "z"}, tools.WriteToolName, "content", "z"},
		{"grok", "other", "list_dir", map[string]any{"path": "/p"}, "list_dir", "path", "/p"},
	} {
		name, input := acpTool2Crush(&acpTool{kind: test.kind, title: test.name, input: test.input})
		require.Equal(t, test.want, name, "%s %s", test.cli, test.name)
		require.Equal(t, test.value, field(t, input, test.field), "%s %s", test.cli, test.name)
	}
	for _, test := range []struct {
		name   string
		params map[string]any
		want   string
	}{
		{"edit_file", map[string]any{"Path": "/p/a.go"}, tools.EditToolName},
		{"read_file", map[string]any{"Path": "/p/a.go"}, tools.ViewToolName},
	} {
		name, input := agyTool(test.name, test.params)
		require.Equal(t, test.want, name, "agy %s", test.name)
		require.Equal(t, "/p/a.go", field(t, input, "file_path"), "agy %s", test.name)
	}
}

// OpenCode sends a shell call first with only its folder; the command
// text follows. Announcing it early left the call without a command, so
// the files it changed were never recorded.
func TestOpenCodeShellCallWaitsForItsCommand(t *testing.T) {
	t.Parallel()
	var events []Event
	emit := func(e Event) error { events = append(events, e); return nil }
	calls := map[string]*acpTool{}
	for _, line := range []string{
		`{"sessionUpdate":"tool_call","toolCallId":"c1","title":"bash","kind":"execute","status":"pending","rawInput":{"cwd":"/p"}}`,
		`{"sessionUpdate":"tool_call_update","toolCallId":"c1","title":"Make a file","kind":"execute","status":"in_progress","rawInput":{"command":"printf hi > made.txt","description":"Make a file","cwd":"/p"}}`,
	} {
		var u acpUpdate
		require.NoError(t, json.Unmarshal([]byte(line), &u))
		require.NoError(t, acpHandleUpdate(u, calls, emit))
	}
	var call *Event
	for i := range events {
		if events[i].Type == EventToolCall {
			require.Nil(t, call, "the call is announced once")
			call = &events[i]
		}
	}
	require.NotNil(t, call)
	require.Equal(t, tools.BashToolName, call.Name)
	require.Equal(t, "printf hi > made.txt", field(t, call.Input, "command"))
}

func field(t *testing.T, input, key string) string {
	t.Helper()
	var in map[string]any
	require.NoError(t, json.Unmarshal([]byte(input), &in))
	s, _ := in[key].(string)
	return s
}

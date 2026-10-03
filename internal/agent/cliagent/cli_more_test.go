package cliagent

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/stretchr/testify/require"
)

// The fake answers each ACP request only after reading it, like the real
// agents do.
func TestACPTurn(t *testing.T) {
	dir := t.TempDir()
	script := `#!/bin/sh
read -r _
echo '{"jsonrpc":"2.0","id":"1","result":{"protocolVersion":1}}'
read -r _
echo '{"jsonrpc":"2.0","id":"2","result":{"sessionId":"ses1"}}'
read -r _
cat <<'EOF'
{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"agent_thought_chunk","content":{"type":"text","text":"hmm"}}}}
{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"tool_call","toolCallId":"c1","title":"bash","kind":"execute","status":"pending","rawInput":{}}}}
{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"tool_call_update","toolCallId":"c1","status":"in_progress","rawInput":{"command":"ls"}}}}
{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"tool_call_update","toolCallId":"c1","status":"completed","content":[{"type":"content","content":{"type":"text","text":"a.txt"}}]}}}
{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"tool_call","toolCallId":"c2","title":"edit","kind":"edit","status":"completed","content":[{"type":"diff","path":"/x","oldText":"a","newText":"b"}]}}}
{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"Done."}}}}
{"jsonrpc":"2.0","id":"3","result":{"stopReason":"end_turn","_meta":{"inputTokens":100,"outputTokens":5,"cachedReadTokens":60}}}
EOF
cat >/dev/null
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "grok"), []byte(script), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	events, err := collect(t, "grok", "")
	require.NoError(t, err)
	require.Equal(t, []EventType{
		EventSession, EventReasoning,
		EventToolStart, EventToolCall, EventToolResult,
		EventToolStart, EventToolCall, EventToolResult,
		EventText, EventUsage,
	}, types(events))
	require.Equal(t, "ses1", events[0].Session)
	require.JSONEq(t, `{"command":"ls","description":""}`, events[3].Input)
	require.Equal(t, "a.txt", events[4].Output)
	require.Equal(t, "edit", events[6].Name)
	require.JSONEq(t, `{"file_path":"/x","old_string":"a","new_string":"b"}`, events[6].Input)
	require.Equal(t, int64(40), events[9].Usage.InputTokens)
}

func TestGrokSelectedEffortReachesTheCLI(t *testing.T) {
	dir := t.TempDir()
	arguments := filepath.Join(dir, "arguments")
	t.Setenv("GROK_TEST_ARGUMENTS", arguments)
	script := `#!/bin/sh
printf '%s\n' "$@" > "$GROK_TEST_ARGUMENTS"
read -r request
echo '{"jsonrpc":"2.0","id":"1","result":{"protocolVersion":1}}'
read -r request
echo '{"jsonrpc":"2.0","id":"2","result":{"sessionId":"effort-test"}}'
read -r request
echo '{"jsonrpc":"2.0","id":"3","result":{"stopReason":"end_turn"}}'
cat >/dev/null
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "grok"), []byte(script), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	model := &Model{Kind: config.TypeGrokCLI, ID: "grok-4.7", Dir: dir}
	for _, effort := range []string{"", "low", "medium", "high", "xhigh"} {
		require.NoError(t, model.Run(t.Context(), Turn{Prompt: "hi", Effort: effort, NoTools: true, Emit: func(Event) error { return nil }}))
		data, err := os.ReadFile(arguments)
		require.NoError(t, err)
		args := strings.Split(strings.TrimSpace(string(data)), "\n")
		want := []string{"agent", "--no-leader", "-m", "grok-4.7"}
		if effort != "" {
			want = append(want, "--reasoning-effort", effort)
		}
		want = append(want, "stdio")
		require.Equal(t, want, args)
	}
}

func TestAGYTurn(t *testing.T) {
	fakeCLI(t, "agy", `{"event":"init","conversation_id":"conv1"}
{"event":"step_update","step_update":{"step_index":1,"state":"DONE","step_type":"agent_response","usage":{"input_tokens":10,"output_tokens":2,"cache_read_tokens":4}}}
{"event":"step_update","step_update":{"step_index":2,"state":"ACTIVE","step_type":"tool","tool_name":"run_command","tool_info":{"parameters":{"CommandLine":"touch a"}}}}
{"event":"step_update","step_update":{"step_index":2,"state":"DONE","step_type":"tool","tool_name":"run_command","tool_info":{"parameters":{"CommandLine":"touch a"}}}}
{"event":"step_update","step_update":{"step_index":3,"state":"ERROR","step_type":"tool","tool_name":"view_file","tool_info":{"parameters":{"AbsolutePath":"/nope"},"error":{"message":"no such file"}}}}
{"event":"step_update","step_update":{"step_index":4,"state":"ACTIVE","step_type":"agent_response","text_delta":"Done."}}
{"event":"result","result":{"status":"SUCCESS"}}
`)
	// Skip the real ~/.local/bin/agy.
	t.Setenv("HOME", t.TempDir())
	events, err := collect(t, "agy", "")
	require.NoError(t, err)
	require.Equal(t, []EventType{
		EventSession, EventUsage,
		EventToolStart, EventToolCall, EventToolResult,
		EventToolStart, EventToolCall, EventToolResult,
		EventText,
	}, types(events))
	require.Equal(t, int64(6), events[1].Usage.InputTokens)
	require.JSONEq(t, `{"command":"touch a","description":""}`, events[3].Input)
	require.True(t, events[7].IsError)

	// An unknown conversation makes agy start a new one silently.
	_, err = collect(t, "agy", "other")
	require.ErrorIs(t, err, ErrResume)
}

func TestClaudeSettingsReachTheCLI(t *testing.T) {
	dir := t.TempDir()
	arguments := filepath.Join(dir, "arguments")
	t.Setenv("CLAUDE_TEST_ARGUMENTS", arguments)
	script := `#!/bin/sh
printf '%s\n' "$@" > "$CLAUDE_TEST_ARGUMENTS"
read -r _
echo '{"type":"system","subtype":"init","session_id":"s1"}'
echo '{"type":"result","subtype":"success","result":"ok"}'
cat >/dev/null
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	for _, tc := range []struct {
		tier      string
		ultracode bool
		settings  string
	}{
		{"", false, ""},
		{"fast", false, `{"fastMode":true}`},
		{"", true, `{"ultracode":true}`},
		{"fast", true, `{"fastMode":true,"ultracode":true}`},
	} {
		model := &Model{Kind: config.TypeClaudeCode, ID: "opus", Dir: dir, ServiceTier: tc.tier, Ultracode: tc.ultracode}
		require.NoError(t, model.Run(t.Context(), Turn{Prompt: "hi", NoTools: true, Emit: func(Event) error { return nil }}))
		data, err := os.ReadFile(arguments)
		require.NoError(t, err)
		args := strings.Split(strings.TrimSpace(string(data)), "\n")
		i := slices.Index(args, "--settings")
		if tc.settings == "" {
			require.Equal(t, -1, i)
			continue
		}
		require.Greater(t, i, -1)
		require.JSONEq(t, tc.settings, args[i+1])
	}
}

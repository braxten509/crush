package cliagent

import (
	"os"
	"path/filepath"
	"testing"

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

func TestAbacusTurn(t *testing.T) {
	fakeCLI(t, "abacusai", `{"type":"system","subtype":"init","session_id":null}
{"type":"event","event":{"type":"conversation_info","conversationId":"ab1"}}
{"type":"event","event":{"type":"thinking_delta","content":"plan"}}
{"type":"event","event":{"type":"tool_call","toolCall":{"id":"b1","name":"bash","args":{"command":"ls"},"endpoint":"bash"}}}
{"type":"event","event":{"type":"file_read","filepath":"/f","toolCallId":"r1"}}
{"type":"event","event":{"type":"tool_call","toolCall":{"id":"r1","name":"read","args":{"path":"/f"},"endpoint":"file_read"}}}
{"type":"event","event":{"type":"tool_result","result":{"toolCallId":"b1","output":"a.txt"}}}
{"type":"event","event":{"type":"tool_call","toolCall":{"id":"e1","name":"edit","args":{"path":"/f","old_str":"a","new_str":"b"},"endpoint":"file_str_replace"}}}
{"type":"event","event":{"type":"file_write_done","toolCallId":"e1"}}
{"type":"event","event":{"type":"text_delta","content":"Done."}}
{"type":"event","event":{"type":"turn_complete"}}
{"type":"result","subtype":"success","is_error":false}
`)
	events, err := collect(t, "abacus", "")
	require.NoError(t, err)
	require.Equal(t, []EventType{
		EventSession, EventReasoning,
		EventToolStart, EventToolCall,
		EventToolStart, EventToolCall, EventToolResult,
		EventToolResult,
		EventToolStart, EventToolCall, EventToolResult,
		EventText,
	}, types(events))
	require.Equal(t, "ab1", events[0].Session)
	require.Equal(t, "view", events[5].Name)
	require.Equal(t, "b1", events[7].ID)
	require.Equal(t, "a.txt", events[7].Output)
	require.JSONEq(t, `{"file_path":"/f","old_string":"a","new_string":"b"}`, events[9].Input)
}

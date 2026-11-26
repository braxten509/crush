package cliagent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/secretguard"
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
		flag := slices.Index(args, "-m")
		require.Greater(t, flag, -1)
		require.Less(t, flag+1, len(args))
		require.Equal(t, "grok-4.7", args[flag+1])
		at := slices.Index(args, "--reasoning-effort")
		if effort == "" {
			require.Equal(t, -1, at)
			continue
		}
		require.Greater(t, at, -1)
		require.Less(t, at+1, len(args))
		require.Equal(t, effort, args[at+1])
	}
}

// AGY runs through its ACP server: the model is set with session/set_model,
// its tool shapes map onto Crush's tools, the context meter gets the
// window AGY reports, and an unknown conversation fails to resume.
func TestAGYTurn(t *testing.T) {
	dir := t.TempDir()
	viewed := filepath.Join(dir, "notes.txt")
	require.NoError(t, os.WriteFile(viewed, []byte("alpha"), 0o644))
	script := `#!/bin/sh
read -r _
echo '{"jsonrpc":"2.0","id":"1","result":{"protocolVersion":1}}'
read -r open
case "$open" in *session/resume*)
  echo '{"jsonrpc":"2.0","id":"2","error":{"code":-32603,"message":"Failed to read session"}}'
  cat >/dev/null; exit 0;;
esac
echo '{"jsonrpc":"2.0","id":"2","result":{"sessionId":"conv1"}}'
read -r model
case "$model" in *'"session/set_model"'*'"modelId":"m"'*) ;; *) exit 9;; esac
echo '{"jsonrpc":"2.0","id":"set-model","result":{}}'
read -r _
cat <<'EOF'
{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"usage_update","used":0,"size":1048576}}}
{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"tool_call","toolCallId":"c1","name":"run_command","kind":"execute","title":"touch a","status":"in_progress","rawInput":{"CommandLine":"touch a","WaitMsBeforeAsync":10000}}}}
{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"tool_call_update","toolCallId":"c1","name":"run_command","kind":"execute","title":"touch a","status":"completed","rawInput":{"CommandLine":"touch a"},"content":[{"type":"content","content":{"type":"text","text":"Command exited with code 0."}}]}}}
{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"tool_call","toolCallId":"c2","name":"read_file","kind":"read","title":"File notes.txt","status":"in_progress","rawInput":{"Path":"VIEWED"}}}}
{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"tool_call_update","toolCallId":"c2","name":"read_file","kind":"read","title":"File notes.txt","status":"completed","rawInput":{"Path":"VIEWED"}}}}
{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"Done."}}}}
{"jsonrpc":"2.0","id":"3","result":{"stopReason":"end_turn","usage":{"inputTokens":10,"outputTokens":2,"cachedReadTokens":4,"thoughtTokens":3}}}
EOF
cat >/dev/null
`
	script = strings.ReplaceAll(script, "VIEWED", viewed)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "agy"), []byte(script), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HOME", t.TempDir()) // skip the real ~/.local/bin/agy

	events, err := collect(t, "agy", "")
	require.NoError(t, err)
	var calls, results []Event
	var limit int64
	var usage *Event
	for i, e := range events {
		switch e.Type {
		case EventToolCall:
			calls = append(calls, e)
		case EventToolResult:
			results = append(results, e)
		case EventContextBudget:
			limit = max(limit, e.ContextLimit)
		case EventUsage:
			usage = &events[i]
		}
	}
	require.Len(t, calls, 2)
	require.Equal(t, "bash", calls[0].Name)
	require.Equal(t, "touch a", field(t, calls[0].Input, "command"))
	require.Equal(t, "view", calls[1].Name)
	require.Len(t, results, 2)
	require.Contains(t, results[1].Metadata, "alpha", "a read shows the file, which AGY leaves out")
	require.EqualValues(t, 1048576, limit)
	require.NotNil(t, usage)
	require.EqualValues(t, 10, usage.Usage.InputTokens)
	require.EqualValues(t, 3, usage.Usage.ReasoningTokens)

	_, err = collect(t, "agy", "other")
	require.ErrorIs(t, err, ErrResume)
}

// AGY reports out-of-usage stops as a warning chat chunk followed by end_turn.
// Turn a trailing out-of-usage warning into an error without emitting it as
// assistant text, while keeping normal warnings and continued turns as text.
func TestAGYUsageWarningEndsTurnAsError(t *testing.T) {
	for _, tc := range []struct {
		name      string
		updates   string
		wantErr   string
		wantTexts []string
	}{
		{
			name: "trailing quota warning becomes an error",
			updates: `{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"⚠ Individual quota reached. Please upgrade your subscription to increase your limits. Resets in 1h20m26s.\n"}}}}
{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"usage_update","used":120,"size":1048576}}}`,
			wantErr: "agy: Individual quota reached. Please upgrade your subscription to increase your limits. Resets in 1h20m26s.",
		},
		{
			name: "warning followed by more text stays normal text",
			updates: `{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"\n⚠ Individual quota reached. Resets in 1h20m26s.\n"}}}}
{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"That was an example."}}}}`,
			wantTexts: []string{"\n⚠ Individual quota reached. Resets in 1h20m26s.\n", "That was an example."},
		},
		{
			name:      "non-usage warning stays normal text",
			updates:   `{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"⚠ The demo plugin needs you to sign in.\n"}}}}`,
			wantTexts: []string{"⚠ The demo plugin needs you to sign in.\n"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			script := `#!/bin/sh
read -r _
echo '{"jsonrpc":"2.0","id":"1","result":{"protocolVersion":1}}'
read -r _
echo '{"jsonrpc":"2.0","id":"2","result":{"sessionId":"conv1"}}'
read -r _
echo '{"jsonrpc":"2.0","id":"set-model","result":{}}'
read -r _
cat <<'EOF'
` + tc.updates + `
{"jsonrpc":"2.0","id":"3","result":{"stopReason":"end_turn"}}
EOF
cat >/dev/null
`
			require.NoError(t, os.WriteFile(filepath.Join(dir, "agy"), []byte(script), 0o755))
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("HOME", t.TempDir())

			events, err := collect(t, "agy", "")
			var texts []string
			for _, e := range events {
				if e.Type == EventText {
					texts = append(texts, e.Text)
				}
			}
			if tc.wantErr != "" {
				require.EqualError(t, err, tc.wantErr)
				require.Empty(t, texts)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.wantTexts, texts)
		})
	}
}

// A message sent while AGY runs a tool goes in at once as another
// session/prompt. AGY answers the prompt it was on straight away and goes on
// with the same turn; the message shows once the tool ends and the model's
// next step starts, and the turn ends with the message's own answer.
func TestAGYSteersDuringATool(t *testing.T) {
	dir := t.TempDir()
	script := `#!/bin/sh
read -r _
echo '{"jsonrpc":"2.0","id":"1","result":{"protocolVersion":1}}'
read -r _
echo '{"jsonrpc":"2.0","id":"2","result":{"sessionId":"conv1"}}'
read -r _
echo '{"jsonrpc":"2.0","id":"set-model","result":{}}'
read -r _
echo '{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"tool_call","toolCallId":"c1","name":"run_command","kind":"execute","title":"sleep 5","status":"in_progress","rawInput":{"CommandLine":"sleep 5"}}}}'
read -r steer
case "$steer" in *'"id":"steer-1"'*PINEAPPLE*) ;; *) exit 8;; esac
cat <<'EOF'
{"jsonrpc":"2.0","id":"3","result":{"stopReason":"end_turn","usage":{"inputTokens":10,"outputTokens":1}}}
{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"tool_call_update","toolCallId":"c1","name":"run_command","kind":"execute","status":"completed","rawInput":{"CommandLine":"sleep 5"},"content":[{"type":"content","content":{"type":"text","text":"done"}}]}}}
{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"usage_update","used":20,"size":1048576}}}
{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"Two."}}}}
{"jsonrpc":"2.0","id":"steer-1","result":{"stopReason":"end_turn","usage":{"inputTokens":5,"outputTokens":2}}}
EOF
cat >/dev/null
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "agy"), []byte(script), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HOME", t.TempDir()) // skip the real ~/.local/bin/agy
	m := &Model{Kind: config.TypeAGYCLI, ID: "m", Dir: t.TempDir()}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	var given atomic.Bool
	var mu sync.Mutex
	var order []EventType
	var confirmed []string
	var usage fantasy.Usage
	err := m.Run(ctx, Turn{
		Prompt: "hi",
		Emit: func(e Event) error {
			mu.Lock()
			defer mu.Unlock()
			order = append(order, e.Type)
			switch e.Type {
			case EventUserMessage:
				confirmed = append(confirmed, e.Text)
			case EventUsage:
				usage = e.Usage
			}
			return nil
		},
		Steer: func() string {
			if given.Swap(true) {
				return ""
			}
			return "PINEAPPLE"
		},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"PINEAPPLE"}, confirmed)
	shown := slices.Index(order, EventUserMessage)
	require.Greater(t, shown, slices.Index(order, EventToolResult), "shown after the tool it waited on")
	require.Less(t, shown, slices.Index(order, EventText), "shown before the step that read it")
	require.EqualValues(t, 15, usage.InputTokens, "both prompts' usage counts")
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
	}{
		{"", false},
		{"fast", false},
		{"", true},
		{"fast", true},
	} {
		model := &Model{Kind: config.TypeClaudeCode, ID: "opus", Dir: dir, ServiceTier: tc.tier, Ultracode: tc.ultracode}
		require.NoError(t, model.Run(t.Context(), Turn{Prompt: "hi", NoTools: true, Emit: func(Event) error { return nil }}))
		data, err := os.ReadFile(arguments)
		require.NoError(t, err)
		args := strings.Split(strings.TrimSpace(string(data)), "\n")
		i := slices.Index(args, "--settings")
		require.Greater(t, i, -1)
		require.Less(t, i+1, len(args))
		var settings struct {
			FastMode  bool `json:"fastMode"`
			Ultracode bool `json:"ultracode"`
			Hooks     struct {
				PreToolUse []struct {
					Hooks []struct {
						Command string `json:"command"`
					} `json:"hooks"`
				} `json:"PreToolUse"`
			} `json:"hooks"`
		}
		require.NoError(t, json.Unmarshal([]byte(args[i+1]), &settings))
		require.Equal(t, tc.tier == "fast", settings.FastMode)
		require.Equal(t, tc.ultracode, settings.Ultracode)
		var commands []string
		for _, entry := range settings.Hooks.PreToolUse {
			for _, hook := range entry.Hooks {
				commands = append(commands, hook.Command)
			}
		}
		require.Contains(t, commands, secretguard.HookCommand())
	}
}

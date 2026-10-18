//go:build linux

package agent

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/agent/cliagent"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/csync"
	"github.com/charmbracelet/crush/internal/db"
	"github.com/charmbracelet/crush/internal/filetracker"
	"github.com/charmbracelet/crush/internal/history"
	"github.com/charmbracelet/crush/internal/lsp"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/permission"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/stretchr/testify/require"
)

// These pin each agent CLI's markers (engine-design.md 5.11): its own ID
// for a Crush message, recorded as the reply arrives, on the right message.
// The fake CLIs speak the same stream formats the real ones do.

// markerEnv is testEnv with its queries kept, so a test can read the
// markers a turn recorded. Its working folder is a temporary one.
func markerEnv(t *testing.T) (fakeEnv, *db.Queries) {
	t.Helper()
	dir := t.TempDir()
	conn, err := db.Connect(t.Context(), t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { conn.Close() })
	q := db.New(conn)
	tracker := filetracker.NewService(q)
	return fakeEnv{
		workingDir:  dir,
		sessions:    session.NewService(q, conn),
		messages:    message.NewService(q),
		permissions: permission.NewPermissionService(dir, true, []string{}),
		history:     history.NewService(q, conn),
		filetracker: &tracker,
		lspClients:  csync.NewMap[string, *lsp.Client](),
	}, q
}

// fakeAgentCLI puts script on PATH as the agent CLI bin.
func fakeAgentCLI(t *testing.T, bin, script string) {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, bin), []byte(script), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// fakeClaude is a Claude Code that takes one prompt and answers it with
// lines, one JSON object per line, then exits.
func fakeClaude(t *testing.T, lines string) {
	t.Helper()
	fakeAgentCLI(t, "claude", `#!/usr/bin/python3
import sys, json
for line in sys.stdin:
    if json.loads(line).get('type') == 'user':
        break
sys.stdout.write(`+fmt.Sprintf("%q", lines)+`)
sys.stdout.flush()
`)
}

// cliChat runs each prompt as a turn of one new chat through the agent CLI
// of kind, and returns the chat's messages and the markers it recorded.
func cliChat(t *testing.T, env fakeEnv, q *db.Queries, kind catwalk.Type, prompts ...string) ([]message.Message, []db.MessageMarker) {
	t.Helper()
	provider := cliagent.NewProvider(kind, env.workingDir, t.TempDir(), env.permissions, env.history, "", false)
	model, err := provider.LanguageModel(t.Context(), "fixture")
	require.NoError(t, err)
	a := testSessionAgent(env, model, &finishStreamModel{text: "fixture"}, "system").(*sessionAgent)
	a.isSubAgent = true // no title request goes to the CLI
	chat, err := env.sessions.Create(t.Context(), "markers")
	require.NoError(t, err)
	t.Cleanup(func() { _ = cliagent.CloseForTree(chat.ID) })
	for _, prompt := range prompts {
		_, err := a.Run(t.Context(), SessionAgentCall{SessionID: chat.ID, Prompt: prompt, NonInteractive: true})
		require.NoError(t, err)
	}
	msgs, err := env.messages.List(t.Context(), chat.ID)
	require.NoError(t, err)
	markers, err := q.MarkersForSession(t.Context(), chat.ID)
	require.NoError(t, err)
	for _, m := range markers {
		require.Equal(t, string(kind), m.CLI)
		require.Equal(t, chat.ID, m.SessionID)
	}
	return msgs, markers
}

// placed describes each marker as "<message>=<marker> <kind> <native>". A
// message is named by its role and number ("assistant 2"), a tool message
// by the call its result answers ("tool b1").
func placed(msgs []message.Message, markers []db.MessageMarker) []string {
	names := map[string]string{}
	count := map[message.MessageRole]int{}
	for _, m := range msgs {
		count[m.Role]++
		names[m.ID] = fmt.Sprintf("%s %d", m.Role, count[m.Role])
		if results := m.ToolResults(); m.Role == message.Tool && len(results) > 0 {
			names[m.ID] = "tool " + results[0].ToolCallID
		}
	}
	var out []string
	for _, mk := range markers {
		out = append(out, fmt.Sprintf("%s=%s %s %s", cmp.Or(names[mk.MessageID], mk.MessageID), mk.Marker, mk.MarkerKind, mk.NativeSession))
	}
	return out
}

// roles lists the chat's message roles in order.
func roles(msgs []message.Message) string {
	var out []string
	for _, m := range msgs {
		out = append(out, string(m.Role))
	}
	return strings.Join(out, " ")
}

// Claude reports its own ID on every message line: the reply lines go on
// their assistant message (the newest one wins), the tool result line on
// the result's tool message.
func TestClaudeMarkersRecorded(t *testing.T) {
	env, q := markerEnv(t)
	fakeClaude(t, `{"type":"system","subtype":"init","session_id":"cs1"}
{"type":"stream_event","session_id":"cs1","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"Let me look."}}}
{"type":"assistant","uuid":"a-text","session_id":"cs1","message":{"content":[{"type":"text","text":"Let me look."}]}}
{"type":"assistant","uuid":"a-tool","session_id":"cs1","message":{"content":[{"type":"tool_use","id":"t1","name":"Read","input":{"file_path":"/a.go"}}]}}
{"type":"user","uuid":"u-1","session_id":"cs1","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"package a"}]}}
{"type":"stream_event","session_id":"cs1","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"Done."}}}
{"type":"assistant","uuid":"a-done","session_id":"cs1","message":{"content":[{"type":"text","text":"Done."}]}}
{"type":"result","subtype":"success","result":"Done.","session_id":"cs1"}
`)
	msgs, markers := cliChat(t, env, q, config.TypeClaudeCode, "Read a.go")
	require.Equal(t, "user assistant tool assistant", roles(msgs))
	require.ElementsMatch(t, []string{
		"assistant 1=a-tool message cs1",
		"tool t1=u-1 message cs1",
		"assistant 2=a-done message cs1",
	}, placed(msgs, markers))
}

// A shell command has no file tracker, yet its result still gets its tool
// marker, on its own tool message: not on the edit's before it, and not
// lost because only tracked tools fill the review's results.
func TestUntrackedToolGetsItsMarker(t *testing.T) {
	env, q := markerEnv(t)
	notes := filepath.Join(env.workingDir, "notes.txt")
	require.NoError(t, os.WriteFile(notes, []byte("a\n"), 0o600))
	fakeClaude(t, `{"type":"system","subtype":"init","session_id":"cs2"}
{"type":"assistant","uuid":"a-edit","session_id":"cs2","message":{"content":[{"type":"tool_use","id":"e1","name":"Edit","input":{"file_path":`+fmt.Sprintf("%q", notes)+`,"old_string":"a","new_string":"b"}}]}}
{"type":"user","uuid":"u-edit","session_id":"cs2","message":{"content":[{"type":"tool_result","tool_use_id":"e1","content":"The file was updated."}]}}
{"type":"assistant","uuid":"a-bash","session_id":"cs2","message":{"content":[{"type":"tool_use","id":"b1","name":"Bash","input":{"command":"echo hi"}}]}}
{"type":"user","uuid":"u-bash","session_id":"cs2","message":{"content":[{"type":"tool_result","tool_use_id":"b1","content":"hi"}]}}
{"type":"stream_event","session_id":"cs2","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"Done."}}}
{"type":"assistant","uuid":"a-done","session_id":"cs2","message":{"content":[{"type":"text","text":"Done."}]}}
{"type":"result","subtype":"success","result":"Done.","session_id":"cs2"}
`)
	msgs, markers := cliChat(t, env, q, config.TypeClaudeCode, "Edit the notes, then say hi")
	require.Equal(t, "user assistant tool tool assistant", roles(msgs))
	got := placed(msgs, markers)
	require.Contains(t, got, "tool b1=u-bash message cs2", "the shell command's own tool message holds its marker")
	require.ElementsMatch(t, []string{
		"assistant 1=a-bash message cs2",
		"tool e1=u-edit message cs2",
		"tool b1=u-bash message cs2",
		"assistant 2=a-done message cs2",
	}, got)
}

// Codex reports one marker per turn, its turn ID, on the turn's last
// assistant message. The app server stays open between the two turns.
func TestCodexTurnMarkers(t *testing.T) {
	env, q := markerEnv(t)
	fakeAgentCLI(t, "codex", `#!/usr/bin/python3
import json, os, sys
state = os.path.join(os.path.dirname(os.path.abspath(__file__)), 'turns')
def send(o): print(json.dumps(o), flush=True)
def item(kind, **fields): return {'item': dict(type=kind, **fields)}
for line in sys.stdin:
    msg = json.loads(line)
    method, mid = msg.get('method'), msg.get('id')
    if method == 'initialize':
        send({'id': mid, 'result': {}})
    elif method == 'hooks/list':
        send({'id': mid, 'result': {'data': []}})
    elif method in ('thread/start', 'thread/resume'):
        send({'id': mid, 'result': {'thread': {'id': 'th1'}}})
    elif method == 'turn/start':
        n = int(open(state).read()) + 1 if os.path.exists(state) else 1
        open(state, 'w').write(str(n))
        turn = 'tu%d' % n
        send({'id': mid, 'result': {'turn': {'id': turn}}})
        if n == 1:
            send({'method': 'item/started', 'params': item('agentMessage', id='m1')})
            send({'method': 'item/agentMessage/delta', 'params': {'itemId': 'm1', 'delta': 'Checking.'}})
            send({'method': 'item/started', 'params': item('commandExecution', id='c1', command='/bin/zsh -lc "echo hi"')})
            send({'method': 'item/completed', 'params': item('commandExecution', id='c1', status='completed', aggregatedOutput='hi\n', exitCode=0)})
        reply = 'r%d' % n
        send({'method': 'item/started', 'params': item('agentMessage', id=reply)})
        send({'method': 'item/agentMessage/delta', 'params': {'itemId': reply, 'delta': 'Reply %d.' % n}})
        send({'method': 'turn/completed', 'params': {'turn': {'id': turn, 'status': 'completed'}}})
        if n == 2:
            break
    elif method and mid is not None:
        send({'id': mid, 'result': {}})
`)
	msgs, markers := cliChat(t, env, q, config.TypeCodexCLI, "Check", "Again")
	require.Equal(t, "user assistant tool assistant user assistant", roles(msgs))
	require.ElementsMatch(t, []string{
		"assistant 2=tu1 turn th1",
		"assistant 3=tu2 turn th1",
	}, placed(msgs, markers))
}

// OpenCode names its own message on each text chunk: one marker per
// message, on the assistant message its text went to.
func TestOpenCodeMessageMarkers(t *testing.T) {
	env, q := markerEnv(t)
	t.Setenv("XDG_CACHE_HOME", t.TempDir()) // OpenCode's bash tool is written there
	fakeAgentCLI(t, "opencode", `#!/usr/bin/python3
import json, sys
def send(o): print(json.dumps(o), flush=True)
def update(u): send({'jsonrpc': '2.0', 'method': 'session/update', 'params': {'sessionId': 'oc1', 'update': u}})
def text(message, words): update({'sessionUpdate': 'agent_message_chunk', 'messageId': message, 'content': {'type': 'text', 'text': words}})
for line in sys.stdin:
    msg = json.loads(line)
    method, mid = msg.get('method'), msg.get('id')
    if method == 'initialize':
        send({'jsonrpc': '2.0', 'id': mid, 'result': {'protocolVersion': 1}})
    elif method == 'session/new':
        send({'jsonrpc': '2.0', 'id': mid, 'result': {'sessionId': 'oc1'}})
    elif method == 'session/prompt':
        text('msg_a', 'Let me ')
        text('msg_a', 'look.')
        update({'sessionUpdate': 'tool_call', 'toolCallId': 'call_1', 'kind': 'read', 'title': 'Read a.go', 'status': 'pending', 'rawInput': {'filePath': '/a.go'}})
        update({'sessionUpdate': 'tool_call_update', 'toolCallId': 'call_1', 'status': 'completed', 'content': [{'type': 'content', 'content': {'type': 'text', 'text': 'package a'}}]})
        text('msg_b', 'Done.')
        send({'jsonrpc': '2.0', 'id': mid, 'result': {'stopReason': 'end_turn'}})
    elif method and mid is not None:
        send({'jsonrpc': '2.0', 'id': mid, 'result': {}})
`)
	msgs, markers := cliChat(t, env, q, config.TypeOpenCodeCLI, "Read a.go")
	require.Equal(t, "user assistant tool assistant", roles(msgs))
	require.ElementsMatch(t, []string{
		"assistant 1=msg_a message oc1",
		"assistant 2=msg_b message oc1",
	}, placed(msgs, markers))
}

// Grok's marker is the number of prompts its session holds once a prompt
// ended: the second turn loads the session, whose replay holds the first
// prompt (in two chunks), and adds its own.
func TestGrokPromptIndexMarkers(t *testing.T) {
	env, q := markerEnv(t)
	fakeAgentCLI(t, "grok", `#!/usr/bin/python3
import json, sys
def send(o): print(json.dumps(o), flush=True)
def update(u): send({'jsonrpc': '2.0', 'method': 'session/update', 'params': {'sessionId': 'g1', 'update': u}})
def chunk(kind, words): update({'sessionUpdate': kind, 'content': {'type': 'text', 'text': words}})
turn = 1
for line in sys.stdin:
    msg = json.loads(line)
    method, mid = msg.get('method'), msg.get('id')
    if method == 'initialize':
        send({'jsonrpc': '2.0', 'id': mid, 'result': {'protocolVersion': 1}})
    elif method == 'session/new':
        send({'jsonrpc': '2.0', 'id': mid, 'result': {'sessionId': 'g1'}})
    elif method == 'session/load':
        turn = 2
        chunk('user_message_chunk', 'First ')
        chunk('user_message_chunk', 'question')
        chunk('agent_message_chunk', 'Reply 1.')
        send({'jsonrpc': '2.0', 'id': mid, 'result': {}})
    elif method == 'session/prompt':
        chunk('agent_message_chunk', 'Reply %d.' % turn)
        send({'jsonrpc': '2.0', 'id': mid, 'result': {'stopReason': 'end_turn'}})
    elif method and mid is not None:
        send({'jsonrpc': '2.0', 'id': mid, 'result': {}})
`)
	msgs, markers := cliChat(t, env, q, config.TypeGrokCLI, "First question", "Second question")
	require.Equal(t, "user assistant user assistant", roles(msgs))
	require.ElementsMatch(t, []string{
		"assistant 1=1 prompt_index g1",
		"assistant 2=2 prompt_index g1",
	}, placed(msgs, markers))
}

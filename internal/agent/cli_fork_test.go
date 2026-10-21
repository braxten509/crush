//go:build linux

package agent

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
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

// These pin the forkers of desktop-design 3.4, as corrected by the live
// checks (docs/evidence/phase4/live-summary.md): the first send in a branch
// forks the AI tool's own memory at the cut, and anything else gets the
// written copy. The fake CLIs speak each tool's protocol and log their
// arguments and every stdin line.

// forkFakeHeader is shared by the fake CLIs: log() appends to the log the
// test reads, bump() counts across processes, so each new session, turn
// and message gets its own name, and FAIL is set when the test wants the
// fork call to fail.
const forkFakeHeader = `#!/usr/bin/python3
import json, os, sys
LOG = %q
FAIL = %v
HERE = os.path.dirname(os.path.abspath(__file__))
def log(o):
    with open(LOG, 'a') as f:
        f.write(json.dumps(o) + '\n')
def bump(name):
    path = os.path.join(HERE, name)
    n = int(open(path).read()) + 1 if os.path.exists(path) else 1
    open(path, 'w').write(str(n))
    return n
def send(o):
    print(json.dumps(o), flush=True)
args = sys.argv[1:]
log({'args': args})
`

// The fake Claude Code names its session cs<n>, a fork cfork<n>, and each
// reply's message u<n>; it serves prompts until its input closes.
const forkFakeClaude = `
def flag(name):
    return args[args.index(name) + 1] if name in args else None
if '--fork-session' in args and FAIL:
    sys.stderr.write('No message found with message.uuid of: %s\n' % flag('--resume-session-at'))
    sys.exit(1)
session = 'cfork%d' % bump('sessions') if '--fork-session' in args else (flag('--resume') or 'cs%d' % bump('sessions'))
for line in sys.stdin:
    log({'stdin': line.rstrip('\n')})
    if json.loads(line).get('type') != 'user':
        continue
    n = bump('replies')
    text = 'Reply %d.' % n
    send({'type': 'system', 'subtype': 'init', 'session_id': session})
    send({'type': 'stream_event', 'session_id': session, 'event': {'type': 'content_block_delta', 'delta': {'type': 'text_delta', 'text': text}}})
    send({'type': 'assistant', 'uuid': 'u%d' % n, 'session_id': session, 'message': {'content': [{'type': 'text', 'text': text}]}})
    send({'type': 'result', 'subtype': 'success', 'result': text, 'session_id': session})
`

// The fake Codex app server names threads th<n>, forks thf<n> and turns
// tu<n>. A prompt whose last line is "USE A TOOL" runs a command first.
const forkFakeCodex = `
thread = None
for line in sys.stdin:
    log({'stdin': line.rstrip('\n')})
    msg = json.loads(line)
    method, mid, params = msg.get('method'), msg.get('id'), msg.get('params') or {}
    if method == 'initialize':
        send({'id': mid, 'result': {}})
    elif method == 'hooks/list':
        send({'id': mid, 'result': {'data': []}})
    elif method == 'thread/start':
        thread = 'th%d' % bump('threads')
        send({'id': mid, 'result': {'thread': {'id': thread}}})
    elif method == 'thread/resume':
        thread = params['threadId']
        send({'id': mid, 'result': {'thread': {'id': thread}}})
    elif method == 'thread/fork':
        if FAIL:
            send({'id': mid, 'error': {'code': -32600, 'message': 'no turn ' + params.get('lastTurnId', '')}})
            continue
        thread = 'thf%d' % bump('threads')
        send({'id': mid, 'result': {'thread': {'id': thread, 'turns': []}}})
    elif method == 'turn/start':
        n = bump('turns')
        turn = 'tu%d' % n
        send({'id': mid, 'result': {'turn': {'id': turn}}})
        text = ''.join(i.get('text', '') for i in params.get('input') or [])
        if text.strip().endswith('USE A TOOL'):
            send({'method': 'item/started', 'params': {'item': {'type': 'agentMessage', 'id': 'pre%d' % n}}})
            send({'method': 'item/agentMessage/delta', 'params': {'itemId': 'pre%d' % n, 'delta': 'Checking %d.' % n}})
            send({'method': 'item/started', 'params': {'item': {'type': 'commandExecution', 'id': 'c%d' % n, 'command': '/bin/zsh -lc "echo hi"'}}})
            send({'method': 'item/completed', 'params': {'item': {'type': 'commandExecution', 'id': 'c%d' % n, 'status': 'completed', 'aggregatedOutput': 'hi\n', 'exitCode': 0}}})
        send({'method': 'item/started', 'params': {'item': {'type': 'agentMessage', 'id': 'r%d' % n}}})
        send({'method': 'item/agentMessage/delta', 'params': {'itemId': 'r%d' % n, 'delta': 'Reply %d.' % n}})
        send({'method': 'turn/completed', 'params': {'turn': {'id': turn, 'status': 'completed'}}})
    elif method and mid is not None:
        send({'id': mid, 'result': {}})
`

// The fake ACP agent (Grok or OpenCode) names sessions g<n>, forks gf<n>.
// It keeps each session's prompt count in a file, so session/load replays
// that many prompts and a fork keeps targetPromptIndex + 1 of them.
const forkFakeACP = `
store = os.path.join(HERE, 'prompts.json')
def counts():
    return json.load(open(store)) if os.path.exists(store) else {}
def save(c):
    json.dump(c, open(store, 'w'))
def update(sid, u):
    send({'jsonrpc': '2.0', 'method': 'session/update', 'params': {'sessionId': sid, 'update': u}})
def chunk(sid, kind, text):
    update(sid, {'sessionUpdate': kind, 'messageId': 'msg%d' % bump('messages'), 'content': {'type': 'text', 'text': text}})
sid = None
for line in sys.stdin:
    log({'stdin': line.rstrip('\n')})
    msg = json.loads(line)
    method, mid, params = msg.get('method'), msg.get('id'), msg.get('params') or {}
    if method == 'initialize':
        send({'jsonrpc': '2.0', 'id': mid, 'result': {'protocolVersion': 1, 'agentCapabilities': {'loadSession': True}}})
    elif method == '_x.ai/session/fork':
        if FAIL:
            send({'jsonrpc': '2.0', 'id': mid, 'error': {'code': -32603, 'message': 'fork failed'}})
            continue
        new = 'gf%d' % bump('sessions')
        c = counts()
        c[new] = min(params['targetPromptIndex'] + 1, c.get(params['sourceSessionId'], 0))
        save(c)
        send({'jsonrpc': '2.0', 'id': mid, 'result': {'newSessionId': new, 'chatMessagesCopied': 2 * c[new]}})
    elif method == 'session/new':
        sid = 'g%d' % bump('sessions')
        send({'jsonrpc': '2.0', 'id': mid, 'result': {'sessionId': sid}})
    elif method == 'session/load':
        sid = params['sessionId']
        for i in range(counts().get(sid, 0)):
            chunk(sid, 'user_message_chunk', 'Prompt %d' % (i + 1))
            chunk(sid, 'agent_message_chunk', 'Answer %d' % (i + 1))
        send({'jsonrpc': '2.0', 'id': mid, 'result': {}})
    elif method == 'session/prompt':
        c = counts()
        c[sid] = c.get(sid, 0) + 1
        save(c)
        chunk(sid, 'agent_message_chunk', 'Reply %d.' % bump('replies'))
        send({'jsonrpc': '2.0', 'id': mid, 'result': {'stopReason': 'end_turn'}})
    elif method and mid is not None:
        send({'jsonrpc': '2.0', 'id': mid, 'result': {}})
`

// The fake AGY names its conversations conv<n>, or keeps --conversation.
const forkFakeAGY = `
def flag(name):
    return args[args.index(name) + 1] if name in args else None
conv = flag('--conversation') or 'conv%d' % bump('sessions')
for line in sys.stdin:
    log({'stdin': line.rstrip('\n')})
    text = 'Reply %d.' % bump('replies')
    send({'event': 'init', 'conversation_id': conv})
    send({'event': 'step_update', 'step_update': {'step_index': 1, 'state': 'ACTIVE', 'step_type': 'agent_response', 'text_delta': text}})
    send({'event': 'step_update', 'step_update': {'step_index': 1, 'state': 'DONE', 'step_type': 'agent_response'}})
    send({'event': 'result', 'result': {'status': 'SUCCESS', 'response': text}})
    break
`

// forkRig runs chats and branches through one agent CLI's fake.
type forkRig struct {
	t     *testing.T
	env   fakeEnv
	q     *db.Queries
	conn  *sql.DB
	a     *sessionAgent
	model *cliagent.Model
	log   string
}

// newForkRig puts the fake CLI of kind on PATH; failFork makes its fork
// call fail.
func newForkRig(t *testing.T, kind catwalk.Type, failFork bool) *forkRig {
	t.Helper()
	bin := map[catwalk.Type]string{config.TypeClaudeCode: "claude", config.TypeCodexCLI: "codex", config.TypeGrokCLI: "grok", config.TypeOpenCodeCLI: "opencode", config.TypeAGYCLI: "agy"}[kind]
	body := map[catwalk.Type]string{config.TypeClaudeCode: forkFakeClaude, config.TypeCodexCLI: forkFakeCodex, config.TypeGrokCLI: forkFakeACP, config.TypeOpenCodeCLI: forkFakeACP, config.TypeAGYCLI: forkFakeAGY}[kind]
	r := &forkRig{t: t, log: filepath.Join(t.TempDir(), "calls.jsonl")}
	fakeAgentCLI(t, bin, fmt.Sprintf(forkFakeHeader, r.log, map[bool]string{true: "True", false: "False"}[failFork])+body)
	// A temporary home: AGY's driver prefers ~/.local/bin/agy over PATH, and
	// no real AI tool may run.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	dir := t.TempDir()
	conn, err := db.Connect(t.Context(), t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { conn.Close() })
	r.conn, r.q = conn, db.New(conn)
	tracker := filetracker.NewService(r.q)
	r.env = fakeEnv{
		workingDir:  dir,
		sessions:    session.NewService(r.q, conn),
		messages:    message.NewService(r.q),
		permissions: permission.NewPermissionService(dir, true, []string{}),
		history:     history.NewService(r.q, conn),
		filetracker: &tracker,
		lspClients:  csync.NewMap[string, *lsp.Client](),
	}
	provider := cliagent.NewProvider(kind, dir, t.TempDir(), r.env.permissions, r.env.history, "", false, "")
	lm, err := provider.LanguageModel(t.Context(), "fixture")
	require.NoError(t, err)
	r.model = lm.(*cliagent.Model)
	r.a = testSessionAgent(r.env, lm, &finishStreamModel{text: "fixture"}, "system").(*sessionAgent)
	r.a.isSubAgent = true // no title request goes to the CLI
	return r
}

// chat starts a chat and sends each prompt in it.
func (r *forkRig) chat(prompts ...string) string {
	r.t.Helper()
	chat, err := r.env.sessions.Create(r.t.Context(), "source")
	require.NoError(r.t, err)
	r.t.Cleanup(func() { _ = cliagent.CloseForTree(chat.ID) })
	for _, prompt := range prompts {
		r.send(chat.ID, prompt)
	}
	return chat.ID
}

func (r *forkRig) send(chatID, prompt string) {
	r.t.Helper()
	_, err := r.a.Run(r.t.Context(), SessionAgentCall{SessionID: chatID, Prompt: prompt, NonInteractive: true})
	require.NoError(r.t, err)
}

func (r *forkRig) messages(chatID string) []message.Message {
	r.t.Helper()
	msgs, err := r.env.messages.List(r.t.Context(), chatID)
	require.NoError(r.t, err)
	return msgs
}

// branch branches chatID at the first word of message cut.
func (r *forkRig) branch(chatID string, cut message.Message) string {
	r.t.Helper()
	text := cut.Content().Text
	word := strings.Fields(text)[0]
	res, err := r.env.messages.(message.BranchService).Branch(r.t.Context(), message.BranchRequest{
		SessionID: chatID, MessageID: cut.ID, HighlightText: word, HighlightStart: 0, HighlightEnd: len(word), Title: word,
	})
	require.NoError(r.t, err)
	r.t.Cleanup(func() { _ = cliagent.CloseForTree(res.SessionID) })
	return res.SessionID
}

// forkCall is one line of the fake's log: a process's arguments, or one
// line it read.
type forkCall struct {
	Args  []string `json:"args"`
	Stdin string   `json:"stdin"`
}

// calls returns the log since the last call, and starts a new one.
func (r *forkRig) calls() []forkCall {
	r.t.Helper()
	data, err := os.ReadFile(r.log)
	if os.IsNotExist(err) {
		return nil
	}
	require.NoError(r.t, err)
	require.NoError(r.t, os.Remove(r.log))
	var out []forkCall
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var c forkCall
		require.NoError(r.t, json.Unmarshal([]byte(line), &c))
		out = append(out, c)
	}
	return out
}

// argv lists each process's arguments, joined by spaces.
func argv(calls []forkCall) []string {
	var out []string
	for _, c := range calls {
		if c.Args != nil {
			out = append(out, strings.Join(c.Args, " "))
		}
	}
	return out
}

// requests returns the JSON-RPC requests of method the fake read.
func requests(t *testing.T, calls []forkCall, method string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, c := range calls {
		var msg struct {
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		if c.Stdin != "" && json.Unmarshal([]byte(c.Stdin), &msg) == nil && msg.Method == method {
			out = append(out, msg.Params)
		}
	}
	return out
}

// promptText is what the fake was asked in its turn: the user line of
// Claude and AGY, turn/start's input for Codex, session/prompt for ACP.
func promptText(t *testing.T, calls []forkCall) string {
	t.Helper()
	var texts []string
	for _, c := range calls {
		var msg struct {
			Type    string `json:"type"`
			Event   string `json:"event"`
			Method  string `json:"method"`
			Message struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
			Params struct {
				Input  []struct{ Text string } `json:"input"`
				Prompt []struct{ Text string } `json:"prompt"`
			} `json:"params"`
		}
		if c.Stdin == "" || json.Unmarshal([]byte(c.Stdin), &msg) != nil {
			continue
		}
		switch {
		case msg.Type == "user" || msg.Event == "user":
			var s string
			if json.Unmarshal(msg.Message.Content, &s) == nil {
				texts = append(texts, s)
				continue
			}
			var blocks []struct{ Text string }
			_ = json.Unmarshal(msg.Message.Content, &blocks)
			for _, b := range blocks {
				texts = append(texts, b.Text)
			}
		case msg.Method == "turn/start":
			for _, i := range msg.Params.Input {
				texts = append(texts, i.Text)
			}
		case msg.Method == "session/prompt":
			for _, p := range msg.Params.Prompt {
				texts = append(texts, p.Text)
			}
		}
	}
	return strings.Join(texts, "\n")
}

// role returns the n-th (1-based) message of role in msgs.
func role(t *testing.T, msgs []message.Message, r message.MessageRole, n int) message.Message {
	t.Helper()
	for _, m := range msgs {
		if m.Role == r {
			if n--; n == 0 {
				return m
			}
		}
	}
	t.Fatalf("no %s message %d", r, n)
	return message.Message{}
}

const (
	writtenCopyIntro = "This conversation started before you joined"
	resumeIntro      = "Since your last reply"
)

// The first send in a branch of a Claude chat forks Claude's session at the
// cut reply's own ID, and later sends resume the fork.
func TestClaudeForkArgs(t *testing.T) {
	r := newForkRig(t, config.TypeClaudeCode, false)
	source := r.chat("First", "Second")
	msgs := r.messages(source)
	require.Equal(t, "user assistant user assistant", roles(msgs))
	branch := r.branch(source, role(t, msgs, message.Assistant, 1)) // "Reply 1.", message u1 of cs1
	r.calls()

	r.send(branch, "Third")
	calls := r.calls()
	require.Equal(t, []string{"--resume cs1 --fork-session --resume-session-at u1"}, forkFlags(argv(calls)))
	prompt := promptText(t, calls)
	require.Contains(t, prompt, "Third")
	require.NotContains(t, prompt, writtenCopyIntro, "the fork needs no written copy")
	require.NotContains(t, prompt, "Second", "the fork keeps nothing after the cut")
	require.Equal(t, "cfork2", r.model.Links.Get(branch, config.TypeClaudeCode).Native)

	require.NoError(t, cliagent.CloseForTree(branch))
	r.send(branch, "Fourth")
	require.Equal(t, []string{"--resume cfork2"}, forkFlags(argv(r.calls())), "a later send resumes the fork")
}

// forkFlags keeps the session flags of each Claude process.
func forkFlags(args []string) []string {
	var out []string
	for _, a := range args {
		fields := strings.Fields(a)
		var kept []string
		for i := 0; i < len(fields); i++ {
			switch fields[i] {
			case "--resume", "--resume-session-at":
				if i+1 < len(fields) {
					kept = append(kept, fields[i], fields[i+1])
					i++
				}
			case "--fork-session":
				kept = append(kept, fields[i])
			}
		}
		out = append(out, strings.Join(kept, " "))
	}
	return out
}

// A cut inside a Codex turn forks at the last whole turn before it;
// the rest of the cut turn goes in once, as the partial-turn note.
func TestCodexForkAtLastWholeTurn(t *testing.T) {
	r := newForkRig(t, config.TypeCodexCLI, false)
	source := r.chat("First", "Second\nUSE A TOOL")
	msgs := r.messages(source)
	require.Equal(t, "user assistant user assistant tool assistant", roles(msgs))
	cut := role(t, msgs, message.Assistant, 2) // "Checking 2.", which ran a command
	require.Equal(t, "Checking 2.", cut.Content().Text)
	branch := r.branch(source, cut)
	require.Equal(t, "user assistant user assistant tool", roles(r.messages(branch)))
	r.calls()

	r.send(branch, "Third")
	calls := r.calls()
	forks := requests(t, calls, "thread/fork")
	require.Len(t, forks, 1)
	require.Equal(t, "th1", forks[0]["threadId"])
	require.Equal(t, "tu1", forks[0]["lastTurnId"], "the last whole turn before the cut")
	require.Empty(t, requests(t, calls, "thread/resume"), "the source thread is never resumed")
	require.Empty(t, requests(t, calls, "thread/start"))

	prompt := promptText(t, calls)
	require.Contains(t, prompt, branchNoteIntro)
	note := prompt[strings.Index(prompt, branchNoteIntro):]
	require.Equal(t, 1, strings.Count(note, "Second"), "the cut turn's prompt goes in once")
	require.Equal(t, 1, strings.Count(note, "Checking 2."), "the cut reply goes in once")
	require.Equal(t, 1, strings.Count(note, "<- bash"), "the cut reply's command result goes in once")
	require.NotContains(t, note, "First", "the whole turn is in the fork, not the note")
	require.NotContains(t, prompt, "Reply 1.")
	require.NotContains(t, prompt, "Reply 2.", "the source's later reply is not the branch's")
	require.NotContains(t, prompt, writtenCopyIntro)
	require.Equal(t, "thf2", r.model.Links.Get(branch, config.TypeCodexCLI).Native)
}

// Grok forks with the 0-based index of the last prompt kept: Crush's
// prompt-count marker minus one. It then loads the new session.
func TestGrokForkPromptIndex(t *testing.T) {
	r := newForkRig(t, config.TypeGrokCLI, false)
	source := r.chat("First", "Second", "Third")
	msgs := r.messages(source)
	require.Equal(t, "user assistant user assistant user assistant", roles(msgs))
	branch := r.branch(source, role(t, msgs, message.Assistant, 2)) // prompt count 2
	r.calls()

	r.send(branch, "Fourth")
	calls := r.calls()
	forks := requests(t, calls, "_x.ai/session/fork")
	require.Len(t, forks, 1)
	require.Equal(t, "g1", forks[0]["sourceSessionId"])
	require.Equal(t, r.env.workingDir, forks[0]["sourceCwd"])
	require.Equal(t, r.env.workingDir, forks[0]["newCwd"])
	require.EqualValues(t, 1, forks[0]["targetPromptIndex"], "the count (2) minus one")
	loads := requests(t, calls, "session/load")
	require.Len(t, loads, 1)
	require.Equal(t, "gf2", loads[0]["sessionId"], "the fork is loaded, not the source")
	require.Empty(t, requests(t, calls, "session/new"))
	prompt := promptText(t, calls)
	require.NotContains(t, prompt, branchNoteIntro, "a cut at a turn's end needs no note")
	require.NotContains(t, prompt, writtenCopyIntro)
	require.NotContains(t, prompt, "Third")

	// The branch counts the fork's prompts on: its reply's marker is 3.
	markers, err := r.q.MarkersForSession(t.Context(), branch)
	require.NoError(t, err)
	last := markers[len(markers)-1]
	require.Equal(t, []string{"gf2", "3"}, []string{last.NativeSession, last.Marker})
}

// A cut on a message without a marker forks at the last earlier marked
// one, and the messages in between go in as the note; with no marker at
// all, the written copy runs.
func TestMissingMarkerForksAtLastMarked(t *testing.T) {
	r := newForkRig(t, config.TypeGrokCLI, false)
	source := r.chat("First", "Second", "Third")
	msgs := r.messages(source)
	second, third := role(t, msgs, message.Assistant, 2), role(t, msgs, message.Assistant, 3)
	// As for a prompt Grok took in mid-turn in older chats: no marker.
	_, err := r.conn.ExecContext(t.Context(), `DELETE FROM cli_message_markers WHERE message_id IN (?, ?)`, second.ID, third.ID)
	require.NoError(t, err)
	branch := r.branch(source, third)
	r.calls()

	r.send(branch, "Fourth")
	calls := r.calls()
	forks := requests(t, calls, "_x.ai/session/fork")
	require.Len(t, forks, 1)
	require.EqualValues(t, 0, forks[0]["targetPromptIndex"], "the last marked prompt is the first")
	prompt := promptText(t, calls)
	require.Contains(t, prompt, branchNoteIntro)
	note := prompt[strings.Index(prompt, branchNoteIntro):]
	for _, words := range []string{"Second", "Reply 2.", "Third", "Reply 3."} {
		require.Equal(t, 1, strings.Count(note, words), "%q goes in the note once", words)
	}
	require.NotContains(t, note, "First")
	require.Less(t, strings.Index(note, "Reply 3."), strings.Index(note, "Fourth"), "the note comes before the prompt")

	// No marker at all: the written copy, in a new session.
	_, err = r.conn.ExecContext(t.Context(), `DELETE FROM cli_message_markers`)
	require.NoError(t, err)
	bare := r.branch(source, second)
	r.calls()
	r.send(bare, "Fifth")
	calls = r.calls()
	require.Empty(t, requests(t, calls, "_x.ai/session/fork"))
	require.Len(t, requests(t, calls, "session/new"), 1)
	require.Contains(t, promptText(t, calls), writtenCopyIntro)
}

// OpenCode's fork could not be checked live, so it has none: its branch
// gets the written copy in a new session.
func TestOpenCodeForkOrNone(t *testing.T) {
	require.ErrorIs(t, cliagent.CanFork(config.TypeOpenCodeCLI, cliagent.MarkerMessage), cliagent.ErrNoFork)
	r := newForkRig(t, config.TypeOpenCodeCLI, false)
	source := r.chat("First", "Second")
	msgs := r.messages(source)
	branch := r.branch(source, role(t, msgs, message.Assistant, 1))
	r.calls()

	r.send(branch, "Third")
	calls := r.calls()
	require.Len(t, requests(t, calls, "session/new"), 1)
	require.Empty(t, requests(t, calls, "session/load"))
	require.Empty(t, requests(t, calls, "_x.ai/session/fork"))
	require.Empty(t, requests(t, calls, "session/fork"))
	prompt := promptText(t, calls)
	require.Contains(t, prompt, writtenCopyIntro)
	require.Contains(t, prompt, "Reply 1.")
	require.NotContains(t, prompt, "Second")
}

// AGY has no fork: its branch always gets the written copy.
func TestAGYWrittenCopy(t *testing.T) {
	require.ErrorIs(t, cliagent.CanFork(config.TypeAGYCLI, cliagent.MarkerStep), cliagent.ErrNoFork)
	r := newForkRig(t, config.TypeAGYCLI, false)
	source := r.chat("First", "Second")
	msgs := r.messages(source)
	branch := r.branch(source, role(t, msgs, message.Assistant, 1))
	r.calls()

	r.send(branch, "Third")
	calls := r.calls()
	args := argv(calls)
	require.Len(t, args, 1)
	require.NotContains(t, args[0], "--conversation", "no session is resumed or forked")
	prompt := promptText(t, calls)
	require.Contains(t, prompt, writtenCopyIntro)
	require.Contains(t, prompt, "Reply 1.")
	require.NotContains(t, prompt, "Second")
}

// A fork call the tool refuses leads to the written copy in a new session;
// the turn succeeds and no link to a fork is saved.
func TestForkFailureFallsBack(t *testing.T) {
	r := newForkRig(t, config.TypeCodexCLI, true)
	source := r.chat("First", "Second")
	msgs := r.messages(source)
	branch := r.branch(source, role(t, msgs, message.Assistant, 1))
	r.calls()

	r.send(branch, "Third")
	calls := r.calls()
	require.Len(t, requests(t, calls, "thread/fork"), 1)
	require.Len(t, requests(t, calls, "thread/start"), 1, "the written copy starts a new thread")
	require.Empty(t, requests(t, calls, "thread/resume"))
	prompt := promptText(t, calls)
	require.Contains(t, prompt, writtenCopyIntro)
	require.Contains(t, prompt, "Reply 1.")
	require.NotContains(t, prompt, "Second")

	msgs = r.messages(branch)
	require.Equal(t, "user assistant user assistant", roles(msgs))
	require.Equal(t, "Reply 3.", msgs[3].Content().Text)
	link := r.model.Links.Get(branch, config.TypeCodexCLI)
	require.Equal(t, "th2", link.Native, "only the written copy's own thread is linked")
	require.Equal(t, msgs[3].ID, link.Through)
}

// Branching and sending in the branch leave the source chat's link and
// native session as they were; the source carries on in its own session.
func TestSourceChatUntouchedByFork(t *testing.T) {
	r := newForkRig(t, config.TypeClaudeCode, false)
	source := r.chat("First", "Second")
	before := r.model.Links.Get(source, config.TypeClaudeCode)
	require.Equal(t, "cs1", before.Native)
	msgs := r.messages(source)
	branch := r.branch(source, role(t, msgs, message.Assistant, 1))
	r.calls()
	r.send(branch, "Third")
	require.Equal(t, []string{"--resume cs1 --fork-session --resume-session-at u1"}, forkFlags(argv(r.calls())),
		"the branch's first send forks the source session")
	r.send(branch, "Fourth")
	require.NotContains(t, strings.Join(argv(r.calls()), "\n"), "--resume cs1", "the source session is only forked, never resumed")
	require.Equal(t, before, r.model.Links.Get(source, config.TypeClaudeCode))
	require.Equal(t, roles(msgs), roles(r.messages(source)))

	require.NoError(t, cliagent.CloseForTree(source))
	r.send(source, "Fifth")
	require.Equal(t, []string{"--resume cs1"}, forkFlags(argv(r.calls())))
	require.Equal(t, "cs1", r.model.Links.Get(source, config.TypeClaudeCode).Native)
	require.NotContains(t, r.messages(source)[len(r.messages(source))-1].Content().Text, "Third")
}

// A branch of a branch forks from the middle branch's own session at its
// own reply, not from the root chat's.
func TestBranchOfBranchForks(t *testing.T) {
	r := newForkRig(t, config.TypeClaudeCode, false)
	root := r.chat("First")
	middle := r.branch(root, role(t, r.messages(root), message.Assistant, 1))
	r.send(middle, "Second") // forks cs1 into cfork2, reply u2
	middleMsgs := r.messages(middle)
	require.Equal(t, "user assistant user assistant", roles(middleMsgs))
	leaf := r.branch(middle, role(t, middleMsgs, message.Assistant, 2))
	r.calls()

	r.send(leaf, "Third")
	flags := forkFlags(argv(r.calls()))
	require.Equal(t, []string{"--resume cfork2 --fork-session --resume-session-at u2"}, flags)
	require.False(t, slices.Contains(flags, "--resume cs1 --fork-session --resume-session-at u1"))
}

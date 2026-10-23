package cliagent

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/agent/tools"
)

// Grok and OpenCode speak the Agent Client Protocol (agentclientprotocol.com):
// JSON-RPC over stdio with session/new|load, session/prompt, streamed
// session/update notifications and session/request_permission requests.
type acpUpdate struct {
	Meta struct {
		AutoCompactThreshold int64 `json:"autoCompactThreshold"`
	} `json:"_meta"`
	SessionUpdate string          `json:"sessionUpdate"`
	Entries       any             `json:"entries"` // plan: the agent's steps
	Size          int64           `json:"size"`    // usage_update: the model's context window
	Content       json.RawMessage `json:"content"`
	ToolCallID    string          `json:"toolCallId"`
	Title         string          `json:"title"`
	Kind          string          `json:"kind"`
	Status        string          `json:"status"`
	RawInput      map[string]any  `json:"rawInput"`
	Locations     []struct {
		Path string `json:"path"`
	} `json:"locations"`
	// MessageID names the agent's own message a chunk belongs to. OpenCode
	// sets it on agent_message_chunk.
	MessageID string `json:"messageId"`
}

type acpContent struct {
	Type    string `json:"type"`
	Text    string `json:"text"`
	Content *struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Path    string `json:"path"`
	OldText string `json:"oldText"`
	NewText string `json:"newText"`
	// TerminalID names the terminal Crush runs a call's command in.
	TerminalID string `json:"terminalId"`
}

type acpUsage struct {
	InputTokens      int64 `json:"inputTokens"`
	OutputTokens     int64 `json:"outputTokens"`
	CachedReadTokens int64 `json:"cachedReadTokens"`
	ReasoningTokens  int64 `json:"reasoningTokens"`
}

// acpTool tracks one tool call while its details stream in.
type acpTool struct {
	kind, title string
	input       map[string]any
	diff        *acpContent
	output      strings.Builder
	announced   bool
	crushName   string
}

const (
	acpInitID    = "1"
	acpSessionID = "2"
	acpPromptID  = "3"
)

// acpRun is one turn of an ACP agent.
type acpRun struct {
	ctx      context.Context
	m        *Model
	t        Turn
	p        *proc
	name     string
	setModel bool

	session atomic.Pointer[string]
	calls   map[string]*acpTool
	// terms runs the commands the agent asks Crush to run; nil when
	// Crush doesn't offer it (see [offersTerminal]).
	terms *acpTerminals
	// loading is set while session/load replays history, which is skipped.
	loading bool
	// loaded is the native session session/load opened, if any.
	loaded string

	// OpenCode folds a session/prompt sent mid-turn into the running turn
	// at its next step, then answers it with the turn's own result. It
	// sends no echo, so a message counts as taken once a tool call ends
	// and the model moves on, or once its prompt is answered. Grok would
	// only queue it as a separate turn.
	steerMu   sync.Mutex
	steerIDs  map[string]string // unanswered steered prompts
	steerN    int
	sent      steered
	prompted  atomic.Bool
	boundary  bool   // a tool call ended since the last confirm
	final     []byte // the turn's result, held while steered prompts are open
	stopSteer func()

	// Markers (engine-design.md 5.11). prompts counts the prompts the
	// native session holds: those session/load replays, then each
	// session/prompt sent. Grok's marker is that count at the end of a
	// prompt. lastMessage is the OpenCode message last reported.
	prompts      int
	replayedUser bool   // the replay's last update was a user chunk
	replayedID   string // its message ID, if any
	lastMessage  string
}

// runACP drives an ACP agent started with args. setModel selects m.ID with
// session/set_model for agents that take no model flag.
func runACP(ctx context.Context, m *Model, t Turn, name string, args []string, setModel bool) error {
	if name == "opencode" && !t.NoTools {
		if env, signal := opencodeEnv(); signal != "" {
			t.Env = append(slices.Clone(t.Env), env...)
			defer os.Remove(signal)
			if t.SessionID != "" {
				defer onBackground(t.SessionID, func() bool { return pressCtrlB(signal) })()
			}
		}
	}
	p, err := m.startTurnProc(t.NoTools, t.Env, name, args...)
	if err != nil {
		return err
	}
	defer p.finish()
	var terms *acpTerminals
	if offersTerminal(m, t, name) {
		terms = newACPTerminals(m.Dir)
		defer terms.closeAll()
		t.Emit = terms.attach(t.Emit)
	}
	t.Emit = p.reviewEvents(t.Emit)
	r := &acpRun{
		ctx: ctx, m: m, t: t, p: p, name: name, setModel: setModel,
		terms:    terms,
		calls:    map[string]*acpTool{},
		loading:  t.Resume != "",
		steerIDs: map[string]string{},
	}

	stop := p.watchCancel(ctx, func() {
		if id := r.session.Load(); id != nil {
			_ = p.send(map[string]any{"jsonrpc": "2.0", "method": "session/cancel", "params": map[string]any{"sessionId": *id}})
		} else {
			p.closeInput()
		}
	})
	defer stop()

	_ = p.send(map[string]any{"jsonrpc": "2.0", "id": acpInitID, "method": "initialize", "params": map[string]any{
		"protocolVersion":    1,
		"clientCapabilities": map[string]any{"fs": map[string]any{"readTextFile": false, "writeTextFile": false}, "terminal": terms != nil},
	}})

	r.stopSteer = func() {}
	if name == "opencode" {
		r.stopSteer = pollSteer(r.t, r.prompted.Load, r.steer)
	}
	defer r.stopSteer()
	for p.lines.Scan() {
		var msg rpcMessage
		if json.Unmarshal(p.lines.Bytes(), &msg) != nil {
			continue
		}
		id := strings.Trim(string(msg.ID), `"`)

		if msg.Method != "" && id != "" {
			go acpRequest(ctx, m, r.t, p, msg, r.terms)
			continue
		}

		var done bool
		var err error
		switch {
		case msg.Method == "" && strings.HasPrefix(id, "steer-"):
			done, err = r.handleSteerResponse(id, msg)
		case msg.Method == "":
			done, err = r.handleResponse(id, msg)
		case msg.Method == "session/update" && r.loading:
			r.countReplayed(msg)
			continue
		case msg.Method != "session/update":
			continue
		default:
			err = r.handleUpdate(msg)
		}
		if done || err != nil {
			return err
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if t.Fork != nil && r.session.Load() == nil {
		return fmt.Errorf("%w: %v", ErrNoFork, exitError(name, p))
	}
	if t.Resume != "" && r.session.Load() == nil {
		return ErrResume
	}
	return exitError(name, p)
}

// openSession loads the native session resume, or starts a new one when
// it is empty.
func (r *acpRun) openSession(resume string) {
	params := map[string]any{"cwd": r.m.Dir, "mcpServers": []any{}}
	method := "session/new"
	if resume != "" {
		method, params["sessionId"] = "session/load", resume
	}
	r.loaded = resume
	_ = r.p.send(map[string]any{"jsonrpc": "2.0", "id": acpSessionID, "method": method, "params": params})
}

// steer sends a prompt queued mid-turn to the running session.
func (r *acpRun) steer(text string) {
	r.steerMu.Lock()
	r.steerN++
	id := "steer-" + strconv.Itoa(r.steerN)
	r.steerIDs[id] = text
	r.steerMu.Unlock()
	r.sent.add(text)
	if r.p.send(map[string]any{"jsonrpc": "2.0", "id": id, "method": "session/prompt", "params": map[string]any{
		"sessionId": *r.session.Load(),
		"prompt":    []any{map[string]any{"type": "text", "text": text}},
	}}) != nil {
		r.steerMu.Lock()
		delete(r.steerIDs, id)
		r.steerMu.Unlock()
		r.sent.take(text)
		return
	}
	r.steerMu.Lock()
	r.prompts++
	r.steerMu.Unlock()
}

// countReplayed counts the user prompts session/load replays. A run of
// user_message_chunk updates is one prompt, or several when their message
// IDs differ.
func (r *acpRun) countReplayed(msg rpcMessage) {
	var params struct {
		Update acpUpdate `json:"update"`
	}
	_ = json.Unmarshal(msg.Params, &params)
	u := params.Update
	if u.SessionUpdate != "user_message_chunk" {
		r.replayedUser = false
		return
	}
	r.steerMu.Lock()
	if !r.replayedUser || (u.MessageID != "" && u.MessageID != r.replayedID) {
		r.prompts++
	}
	r.steerMu.Unlock()
	r.replayedUser, r.replayedID = true, u.MessageID
}

// emitPromptMarker reports Grok's prompt count once a prompt ended: the
// index Grok's own rewind works with.
func (r *acpRun) emitPromptMarker() error {
	id := r.session.Load()
	if r.name != "grok" || !r.t.marks() || id == nil {
		return nil
	}
	r.steerMu.Lock()
	count := r.prompts
	r.steerMu.Unlock()
	return r.t.Emit(Event{Type: EventMarker, Marker: &Marker{Native: *id, ID: strconv.Itoa(count), Kind: MarkerPromptIndex, For: MarkerForAssistant}})
}

// emitMessageMarker reports OpenCode's own ID for the message a text chunk
// belongs to, once per message.
func (r *acpRun) emitMessageMarker(u acpUpdate) error {
	id := r.session.Load()
	if r.name != "opencode" || !r.t.marks() || id == nil || u.SessionUpdate != "agent_message_chunk" ||
		u.MessageID == "" || u.MessageID == r.lastMessage || acpText(u.Content) == "" {
		return nil
	}
	r.lastMessage = u.MessageID
	return r.t.Emit(Event{Type: EventMarker, Marker: &Marker{Native: *id, ID: u.MessageID, Kind: MarkerMessage, For: MarkerForAssistant}})
}

// confirm reports the steered prompts the agent took in.
func (r *acpRun) confirm(texts ...string) error {
	for _, text := range texts {
		if r.sent.take(text) {
			if err := r.t.Emit(Event{Type: EventUserMessage, Text: text}); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *acpRun) confirmAll() error {
	r.steerMu.Lock()
	var texts []string
	for _, text := range r.steerIDs {
		texts = append(texts, text)
	}
	r.steerMu.Unlock()
	return r.confirm(texts...)
}

func (r *acpRun) openSteers() int {
	r.steerMu.Lock()
	defer r.steerMu.Unlock()
	return len(r.steerIDs)
}

// handleSteerResponse handles the answer to a steered prompt. done reports
// that the turn ended, with err.
func (r *acpRun) handleSteerResponse(id string, msg rpcMessage) (done bool, err error) {
	r.steerMu.Lock()
	text := r.steerIDs[id]
	delete(r.steerIDs, id)
	r.steerMu.Unlock()
	if msg.Error != nil {
		r.sent.take(text) // not taken; Crush runs it next
	} else if err := r.confirm(text); err != nil {
		return true, err
	}
	if r.final != nil && r.openSteers() == 0 {
		return true, acpFinish(r.name, r.final, r.t.Emit)
	}
	return false, nil
}

// handleResponse handles the agent's response to one of Crush's requests.
// done reports that the turn ended, with err.
func (r *acpRun) handleResponse(id string, msg rpcMessage) (done bool, err error) {
	if msg.Error != nil {
		if (id == acpForkID || id == acpSessionID) && r.t.Fork != nil {
			return true, fmt.Errorf("%w: %s: %s", ErrNoFork, r.name, msg.Error.Message)
		}
		if id == acpSessionID && r.t.Resume != "" {
			return true, ErrResume
		}
		return true, fmt.Errorf("%s: %s", r.name, msg.Error.Message)
	}
	switch id {
	case acpInitID:
		if r.t.Fork != nil {
			params, err := grokForkParams(*r.t.Fork, r.m.Dir)
			if err != nil {
				return true, err
			}
			_ = r.p.send(map[string]any{"jsonrpc": "2.0", "id": acpForkID, "method": grokForkMethod, "params": params})
			return false, nil
		}
		r.openSession(r.t.Resume)
	case acpForkID:
		// The branch continues the new session: load it, as a resume does.
		forked, err := grokForkSession(msg.Result)
		if err != nil {
			return true, err
		}
		r.loading = true
		r.openSession(forked)
	case acpSessionID:
		r.loading = false
		var res struct {
			SessionID string `json:"sessionId"`
		}
		_ = json.Unmarshal(msg.Result, &res)
		sid := cmp.Or(res.SessionID, r.loaded)
		r.session.Store(&sid)
		if err := r.t.Emit(Event{Type: EventSession, Session: sid}); err != nil {
			return true, err
		}
		if r.setModel {
			_ = r.p.send(map[string]any{"jsonrpc": "2.0", "id": "set-model", "method": "session/set_model", "params": map[string]any{"sessionId": sid, "modelId": r.m.ID}})
		}
		if r.name == "opencode" && r.t.Effort != "" {
			_ = r.p.send(map[string]any{"jsonrpc": "2.0", "id": "set-effort", "method": "session/set_config_option", "params": map[string]any{"sessionId": sid, "configId": "effort", "value": r.t.Effort}})
		}
		_ = r.p.send(map[string]any{"jsonrpc": "2.0", "id": acpPromptID, "method": "session/prompt", "params": map[string]any{
			"sessionId": sid,
			"prompt":    []any{map[string]any{"type": "text", "text": withImagePaths(r.t.Prompt, r.t.Attachments)}},
		}})
		r.steerMu.Lock()
		r.prompts++
		r.steerMu.Unlock()
		r.prompted.Store(true)
	case acpPromptID:
		r.stopSteer()
		if err := r.emitPromptMarker(); err != nil {
			return true, err
		}
		if r.openSteers() > 0 {
			// Their answers follow; one may even run as its own turn.
			r.final = msg.Result
			return false, nil
		}
		return true, acpFinish(r.name, msg.Result, r.t.Emit)
	}
	return false, nil
}

// handleUpdate handles a session/update notification.
func (r *acpRun) handleUpdate(msg rpcMessage) error {
	var params struct {
		Update acpUpdate `json:"update"`
	}
	_ = json.Unmarshal(msg.Params, &params)
	switch u := params.Update; {
	case u.SessionUpdate == "tool_call_update" && (u.Status == "completed" || u.Status == "failed"):
		r.boundary = true
	case u.SessionUpdate == "agent_message_chunk", u.SessionUpdate == "agent_thought_chunk", u.SessionUpdate == "tool_call":
		// The model's next step, which has the messages sent before it.
		if (r.boundary || r.final != nil) && r.sent.pending() > 0 {
			if err := r.confirmAll(); err != nil {
				return err
			}
		}
		r.boundary = false
	}
	if r.name == "opencode" && params.Update.SessionUpdate == "usage_update" && params.Update.Size > 0 {
		if err := r.t.Emit(openCodeContextBudget(params.Update.Size)); err != nil {
			return err
		}
	}
	if r.terms != nil {
		var parts []acpContent
		_ = json.Unmarshal(params.Update.Content, &parts)
		for _, c := range parts {
			if c.Type == "terminal" && c.TerminalID != "" {
				r.terms.claim(c.TerminalID, params.Update.ToolCallID)
			}
		}
	}
	if err := acpHandleUpdate(params.Update, r.calls, r.t.Emit); err != nil {
		return err
	}
	return r.emitMessageMarker(params.Update)
}

// acpFinish reports a finished session/prompt's usage and stop reason.
func acpFinish(name string, result json.RawMessage, emit func(Event) error) error {
	var res struct {
		StopReason string    `json:"stopReason"`
		Usage      *acpUsage `json:"usage"`
		Meta       *acpUsage `json:"_meta"`
	}
	_ = json.Unmarshal(result, &res)
	if u := acpTurnUsage(res.Usage, res.Meta); u != nil {
		if err := emit(Event{Type: EventUsage, Usage: *u}); err != nil {
			return err
		}
	}
	switch res.StopReason {
	case "cancelled":
		return context.Canceled
	case "refusal":
		return errors.New(name + " refused to continue")
	}
	return nil
}

func acpHandleUpdate(u acpUpdate, calls map[string]*acpTool, emit func(Event) error) error {
	if u.Meta.AutoCompactThreshold > 0 {
		if err := emit(Event{Type: EventContextBudget, ContextLimit: u.Meta.AutoCompactThreshold}); err != nil {
			return err
		}
	}
	switch u.SessionUpdate {
	case "plan":
		if input, ok := todoInput(u.Entries); ok {
			return emit(Event{Type: EventTodos, Input: input})
		}
	case "agent_message_chunk":
		if c := acpText(u.Content); c != "" {
			return emit(Event{Type: EventText, Text: c})
		}
	case "agent_thought_chunk":
		if c := acpText(u.Content); c != "" {
			return emit(Event{Type: EventReasoning, Text: c})
		}
	case "tool_call", "tool_call_update":
		tc := calls[u.ToolCallID]
		if tc == nil {
			tc = &acpTool{}
			calls[u.ToolCallID] = tc
		}
		tc.kind = cmp.Or(u.Kind, tc.kind)
		if tc.title == "" || u.SessionUpdate == "tool_call" {
			tc.title = cmp.Or(u.Title, tc.title)
		}
		if len(u.RawInput) > 0 {
			tc.input = u.RawInput
		}
		if tc.input == nil && len(u.Locations) > 0 {
			tc.input = map[string]any{"path": u.Locations[0].Path}
		}
		var parts []acpContent
		_ = json.Unmarshal(u.Content, &parts)
		for i, c := range parts {
			if c.Type == "diff" {
				tc.diff = &parts[i]
			}
		}
		done := u.Status == "completed" || u.Status == "failed"
		// Announce once the input is known; some agents only fill it in
		// when the call starts running. OpenCode sends a shell call first
		// with only its folder, so a command waits for its text.
		if !tc.announced && (tc.ready() || tc.diff != nil || done) {
			tc.announced = true
			name, input := acpTool2Crush(tc)
			tc.crushName = name
			if err := emit(Event{Type: EventToolStart, ID: u.ToolCallID, Name: name}); err != nil {
				return err
			}
			if err := emit(Event{Type: EventToolCall, ID: u.ToolCallID, Name: name, Input: input}); err != nil {
				return err
			}
		}
		if done {
			out := ""
			for _, c := range parts {
				if c.Content != nil && c.Content.Type == "text" {
					out += c.Content.Text
				}
			}
			failed := u.Status == "failed"
			meta := ""
			if !failed {
				_, input := acpTool2Crush(tc)
				meta = resultMetadata(tc.crushName, input, out)
				if strings.Contains(out, openCodeBackgroundMark) {
					meta = markBackground(tc.crushName, meta)
				}
			}
			delete(calls, u.ToolCallID)
			return emit(Event{Type: EventToolResult, ID: u.ToolCallID, Name: tc.crushName, Output: out, Metadata: meta, IsError: failed})
		}
	}
	return nil
}

// ready reports whether the call's input says enough to announce it.
func (tc *acpTool) ready() bool {
	if len(tc.input) == 0 {
		return false
	}
	return toolKind(tc.kind, tc.title) != "execute" || inputField(tc.input, commandFields...) != ""
}

// acpTool2Crush maps an ACP tool call onto the Crush tool with the same job,
// using its kind (see [toolKind]) and the common spellings of its input
// fields.
func acpTool2Crush(tc *acpTool) (string, string) {
	in := func(keys ...string) string { return inputField(tc.input, keys...) }
	if input, ok := todoInput(tc.input["todos"]); ok {
		return tools.TodosToolName, input
	}
	path := in(pathFields...)
	switch toolKind(tc.kind, tc.title) {
	case "execute":
		if cmd := in(commandFields...); cmd != "" {
			return tools.BashToolName, marshal(map[string]string{"command": cmd, "description": in("description")})
		}
	case "read":
		if path != "" {
			return tools.ViewToolName, marshal(map[string]string{"file_path": path})
		}
	case "edit":
		if d := tc.diff; d != nil {
			if d.OldText == "" && in("old_string", "oldString") == "" {
				return tools.WriteToolName, marshal(tools.WriteParams{FilePath: cmp.Or(d.Path, path), Content: d.NewText})
			}
			return tools.EditToolName, marshal(tools.EditParams{FilePath: cmp.Or(d.Path, path), OldString: d.OldText, NewString: d.NewText})
		}
		if content := in("content"); content != "" && path != "" {
			return tools.WriteToolName, marshal(tools.WriteParams{FilePath: path, Content: content})
		}
		if path != "" {
			return tools.EditToolName, marshal(tools.EditParams{FilePath: path, OldString: in("old_string", "oldString"), NewString: in("new_string", "newString")})
		}
	case "fetch":
		if url := in("url"); url != "" {
			return tools.WebFetchToolName, marshal(map[string]string{"url": url})
		}
	}
	name := cmp.Or(tc.title, tc.kind, "tool")
	if tc.input == nil {
		return name, "{}"
	}
	return name, marshal(tc.input)
}

func acpText(raw json.RawMessage) string {
	var c acpContent
	_ = json.Unmarshal(raw, &c)
	if c.Type == "text" {
		return c.Text
	}
	return ""
}

// acpTurnUsage prefers the standard usage field (input excludes cache
// reads); Grok instead reports its last request in _meta, input included.
func acpTurnUsage(usage, meta *acpUsage) *fantasy.Usage {
	switch {
	case usage != nil && usage.InputTokens+usage.CachedReadTokens > 0:
		return &fantasy.Usage{InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens, CacheReadTokens: usage.CachedReadTokens, ReasoningTokens: usage.ReasoningTokens}
	case meta != nil && meta.InputTokens > 0:
		return &fantasy.Usage{InputTokens: meta.InputTokens - meta.CachedReadTokens, OutputTokens: meta.OutputTokens, CacheReadTokens: meta.CachedReadTokens, ReasoningTokens: meta.ReasoningTokens}
	}
	return nil
}

// offersTerminal reports whether Crush runs the agent's commands itself:
// only Grok's, whose long-lived shell hides which command changed what.
// Never for a read-only turn, whose commands must stay in its sandbox, or
// a turn without tools.
func offersTerminal(m *Model, t Turn, name string) bool {
	return name == "grok" && !m.ReadOnly && !t.NoTools
}

// acpRequest answers an agent request. Permission prompts go to Crush's
// dialog, the answer picked from the options the agent offered; terminal
// requests run the agent's commands (acp_terminal.go).
func acpRequest(ctx context.Context, m *Model, t Turn, p *proc, msg rpcMessage, terms *acpTerminals) {
	if terms != nil && strings.HasPrefix(msg.Method, "terminal/") {
		result, err := terms.handle(ctx, msg.Method, msg.Params)
		if err != nil {
			_ = p.send(map[string]any{"jsonrpc": "2.0", "id": msg.ID, "error": map[string]any{"code": -32603, "message": err.Error()}})
			return
		}
		_ = p.send(map[string]any{"jsonrpc": "2.0", "id": msg.ID, "result": result})
		return
	}
	if msg.Method != "session/request_permission" {
		_ = p.send(map[string]any{"jsonrpc": "2.0", "id": msg.ID, "error": map[string]any{"code": -32601, "message": "not supported by Crush"}})
		return
	}
	var params struct {
		ToolCall acpUpdate `json:"toolCall"`
		Options  []struct {
			OptionID string `json:"optionId"`
			Kind     string `json:"kind"`
		} `json:"options"`
	}
	_ = json.Unmarshal(msg.Params, &params)
	tc := &acpTool{kind: params.ToolCall.Kind, title: params.ToolCall.Title, input: params.ToolCall.RawInput}
	var parts []acpContent
	_ = json.Unmarshal(params.ToolCall.Content, &parts)
	for i, c := range parts {
		if c.Type == "diff" {
			tc.diff = &parts[i]
		}
	}
	name, input := acpTool2Crush(tc)
	allowed := !t.NoTools && m.approve(ctx, t.SessionID, params.ToolCall.ToolCallID, name, input)
	want := "reject_once"
	if allowed {
		want = "allow_once"
	}
	for _, o := range params.Options {
		if o.Kind == want {
			_ = p.send(map[string]any{"jsonrpc": "2.0", "id": msg.ID, "result": map[string]any{"outcome": map[string]any{"outcome": "selected", "optionId": o.OptionID}}})
			return
		}
	}
	_ = p.send(map[string]any{"jsonrpc": "2.0", "id": msg.ID, "result": map[string]any{"outcome": map[string]any{"outcome": "cancelled"}}})
}

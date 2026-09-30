package cliagent

import (
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
	SessionUpdate string          `json:"sessionUpdate"`
	Content       json.RawMessage `json:"content"`
	ToolCallID    string          `json:"toolCallId"`
	Title         string          `json:"title"`
	Kind          string          `json:"kind"`
	Status        string          `json:"status"`
	RawInput      map[string]any  `json:"rawInput"`
	Locations     []struct {
		Path string `json:"path"`
	} `json:"locations"`
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
	p, err := startProcEnv(m.Dir, t.Env, name, args...)
	if err != nil {
		return err
	}
	defer p.finish()

	var session atomic.Pointer[string]
	stop := p.watchCancel(ctx, func() {
		if id := session.Load(); id != nil {
			_ = p.send(map[string]any{"jsonrpc": "2.0", "method": "session/cancel", "params": map[string]any{"sessionId": *id}})
		} else {
			p.closeInput()
		}
	})
	defer stop()

	_ = p.send(map[string]any{"jsonrpc": "2.0", "id": acpInitID, "method": "initialize", "params": map[string]any{
		"protocolVersion":    1,
		"clientCapabilities": map[string]any{"fs": map[string]any{"readTextFile": false, "writeTextFile": false}, "terminal": false},
	}})

	calls := map[string]*acpTool{}
	loading := t.Resume != "" // session/load replays history first; skip it

	// OpenCode folds a session/prompt sent mid-turn into the running turn
	// at its next step, then answers it with the turn's own result. It
	// sends no echo, so a message counts as taken once a tool call ends
	// and the model moves on, or once its prompt is answered. Grok would
	// only queue it as a separate turn.
	var (
		steerMu  sync.Mutex
		steerIDs = map[string]string{} // unanswered steered prompts
		steerN   int
		sent     steered
		prompted atomic.Bool
		boundary bool   // a tool call ended since the last confirm
		final    []byte // the turn's result, held while steered prompts are open
	)
	stopSteer := func() {}
	if name == "opencode" {
		stopSteer = pollSteer(t, prompted.Load, func(text string) {
			steerMu.Lock()
			steerN++
			id := "steer-" + strconv.Itoa(steerN)
			steerIDs[id] = text
			steerMu.Unlock()
			sent.add(text)
			if p.send(map[string]any{"jsonrpc": "2.0", "id": id, "method": "session/prompt", "params": map[string]any{
				"sessionId": *session.Load(),
				"prompt":    []any{map[string]any{"type": "text", "text": text}},
			}}) != nil {
				steerMu.Lock()
				delete(steerIDs, id)
				steerMu.Unlock()
				sent.take(text)
			}
		})
	}
	defer stopSteer()
	confirm := func(texts ...string) error {
		for _, text := range texts {
			if sent.take(text) {
				if err := t.Emit(Event{Type: EventUserMessage, Text: text}); err != nil {
					return err
				}
			}
		}
		return nil
	}
	confirmAll := func() error {
		steerMu.Lock()
		var texts []string
		for _, text := range steerIDs {
			texts = append(texts, text)
		}
		steerMu.Unlock()
		return confirm(texts...)
	}
	openSteers := func() int {
		steerMu.Lock()
		defer steerMu.Unlock()
		return len(steerIDs)
	}
	for p.lines.Scan() {
		var msg rpcMessage
		if json.Unmarshal(p.lines.Bytes(), &msg) != nil {
			continue
		}
		id := strings.Trim(string(msg.ID), `"`)

		if msg.Method != "" && id != "" {
			go acpRequest(ctx, m, t, p, msg)
			continue
		}

		if msg.Method == "" && strings.HasPrefix(id, "steer-") {
			steerMu.Lock()
			text := steerIDs[id]
			delete(steerIDs, id)
			steerMu.Unlock()
			if msg.Error != nil {
				sent.take(text) // not taken; Crush runs it next
			} else if err := confirm(text); err != nil {
				return err
			}
			if final != nil && openSteers() == 0 {
				return acpFinish(name, final, t.Emit)
			}
			continue
		}

		if msg.Method == "" {
			if msg.Error != nil {
				if id == acpSessionID && t.Resume != "" {
					return ErrResume
				}
				return fmt.Errorf("%s: %s", name, msg.Error.Message)
			}
			switch id {
			case acpInitID:
				params := map[string]any{"cwd": m.Dir, "mcpServers": []any{}}
				method := "session/new"
				if t.Resume != "" {
					method, params["sessionId"] = "session/load", t.Resume
				}
				_ = p.send(map[string]any{"jsonrpc": "2.0", "id": acpSessionID, "method": method, "params": params})
			case acpSessionID:
				loading = false
				var res struct {
					SessionID string `json:"sessionId"`
				}
				_ = json.Unmarshal(msg.Result, &res)
				sid := cmpOr(res.SessionID, t.Resume)
				session.Store(&sid)
				if err := t.Emit(Event{Type: EventSession, Session: sid}); err != nil {
					return err
				}
				if setModel {
					_ = p.send(map[string]any{"jsonrpc": "2.0", "id": "set-model", "method": "session/set_model", "params": map[string]any{"sessionId": sid, "modelId": m.ID}})
				}
				_ = p.send(map[string]any{"jsonrpc": "2.0", "id": acpPromptID, "method": "session/prompt", "params": map[string]any{
					"sessionId": sid,
					"prompt":    []any{map[string]any{"type": "text", "text": withImagePaths(t.Prompt, t.Attachments)}},
				}})
				prompted.Store(true)
			case acpPromptID:
				stopSteer()
				if openSteers() > 0 {
					// Their answers follow; one may even run as its own turn.
					final = msg.Result
					continue
				}
				return acpFinish(name, msg.Result, t.Emit)
			}
			continue
		}

		if msg.Method != "session/update" || loading {
			continue
		}
		var params struct {
			Update acpUpdate `json:"update"`
		}
		_ = json.Unmarshal(msg.Params, &params)
		switch u := params.Update; {
		case u.SessionUpdate == "tool_call_update" && (u.Status == "completed" || u.Status == "failed"):
			boundary = true
		case u.SessionUpdate == "agent_message_chunk", u.SessionUpdate == "agent_thought_chunk", u.SessionUpdate == "tool_call":
			// The model's next step, which has the messages sent before it.
			if (boundary || final != nil) && sent.pending() > 0 {
				if err := confirmAll(); err != nil {
					return err
				}
			}
			boundary = false
		}
		if err := acpHandleUpdate(params.Update, calls, t.Emit); err != nil {
			return err
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if t.Resume != "" && session.Load() == nil {
		return ErrResume
	}
	return exitError(name, p)
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
	switch u.SessionUpdate {
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
		tc.kind = cmpOr(u.Kind, tc.kind)
		if tc.title == "" || u.SessionUpdate == "tool_call" {
			tc.title = cmpOr(u.Title, tc.title)
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
		// when the call starts running.
		if !tc.announced && (len(tc.input) > 0 || tc.diff != nil || done) {
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

// acpTool2Crush maps an ACP tool call onto the Crush tool with the same job,
// using its standard kind and the common spellings of its input fields.
func acpTool2Crush(tc *acpTool) (string, string) {
	in := func(keys ...string) string {
		for _, k := range keys {
			if s, ok := tc.input[k].(string); ok && s != "" {
				return s
			}
		}
		return ""
	}
	path := in("file_path", "filePath", "target_file", "path", "absolute_path")
	switch tc.kind {
	case "execute":
		if cmd := in("command", "cmd"); cmd != "" {
			return tools.BashToolName, marshal(map[string]string{"command": cmd, "description": in("description")})
		}
	case "read":
		if path != "" {
			return tools.ViewToolName, marshal(map[string]string{"file_path": path})
		}
	case "edit":
		if d := tc.diff; d != nil {
			if d.OldText == "" && in("old_string", "oldString") == "" {
				return tools.WriteToolName, marshal(tools.WriteParams{FilePath: cmpOr(d.Path, path), Content: d.NewText})
			}
			return tools.EditToolName, marshal(tools.EditParams{FilePath: cmpOr(d.Path, path), OldString: d.OldText, NewString: d.NewText})
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
	name := cmpOr(tc.title, tc.kind, "tool")
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

// acpRequest answers an agent request. Permission prompts go to Crush's
// dialog, the answer picked from the options the agent offered.
func acpRequest(ctx context.Context, m *Model, t Turn, p *proc, msg rpcMessage) {
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

func cmpOr(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

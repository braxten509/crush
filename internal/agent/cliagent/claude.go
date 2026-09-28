package cliagent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/config"
)

// Claude Code's stream-json protocol, as used by its Agent SDKs: Messages
// API stream events wrapped in session events, plus control requests for
// tool permissions. See `claude --help` (--input-format/--output-format).
type claudeLine struct {
	Type      string                 `json:"type"`
	Subtype   string                 `json:"subtype"`
	SessionID string                 `json:"session_id"`
	Event     *claudeEvent           `json:"event"`
	Message   *claudeMessage         `json:"message"`
	RequestID string                 `json:"request_id"`
	Request   *claudeRequest         `json:"request"`
	IsError   bool                   `json:"is_error"`
	Result    string                 `json:"result"`
	Errors    []string               `json:"errors"`
	Origin    *struct{ Kind string } `json:"origin"`
	IsReplay  bool                   `json:"isReplay"`
}

type claudeEvent struct {
	Type         string `json:"type"`
	ContentBlock struct {
		Type string `json:"type"`
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"content_block"`
	Delta struct {
		Type     string `json:"type"`
		Text     string `json:"text"`
		Thinking string `json:"thinking"`
	} `json:"delta"`
	Usage *claudeUsage `json:"usage"`
}

type claudeMessage struct {
	Content json.RawMessage `json:"content"`
}

type claudeBlock struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
	Text      string          `json:"text"`
}

type claudeRequest struct {
	Subtype   string          `json:"subtype"`
	ToolName  string          `json:"tool_name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
}

type claudeUsage struct {
	InputTokens   int64 `json:"input_tokens"`
	OutputTokens  int64 `json:"output_tokens"`
	CacheCreation int64 `json:"cache_creation_input_tokens"`
	CacheRead     int64 `json:"cache_read_input_tokens"`
}

// claudeIdle is how long a finished session's Claude process stays open for
// the next prompt.
const claudeIdle = 15 * time.Minute

// claudeKey is what a live Claude process was started with; a turn that
// needs anything else starts a new one.
type claudeKey struct {
	dir, model, effort string
	bypass             bool
}

// claudeLive is a Claude process kept open between the turns of one Crush
// session, as the interactive CLI does, so follow-ups skip its startup
// (settings, hooks, MCP servers).
type claudeLive struct {
	p       *proc
	lines   <-chan []byte
	key     claudeKey
	native  string
	idle    chan struct{} // closed to end the between-turns drain
	drained chan struct{}
	dead    atomic.Bool
	timer   *time.Timer
}

// claudeSessions holds the live processes, one per Crush session.
var claudeSessions = struct {
	mu sync.Mutex
	m  map[string]*claudeLive
}{m: map[string]*claudeLive{}}

// takeClaude hands over the session's live process if it can run this turn,
// closing it otherwise.
func takeClaude(sessionID string, key claudeKey, resume string) *claudeLive {
	claudeSessions.mu.Lock()
	l := claudeSessions.m[sessionID]
	delete(claudeSessions.m, sessionID)
	claudeSessions.mu.Unlock()
	if l == nil {
		return nil
	}
	l.timer.Stop()
	close(l.idle)
	<-l.drained
	if l.dead.Load() || l.key != key || resume == "" || resume != l.native {
		l.p.finish()
		return nil
	}
	return l
}

// keepClaude parks a process after a finished turn until the session's next
// prompt, or closes it after [claudeIdle].
func keepClaude(sessionID string, l *claudeLive) {
	l.idle, l.drained = make(chan struct{}), make(chan struct{})
	go func() {
		// Output between turns (a background task finishing) has no turn to
		// show it in.
		// ponytail: dropped; relay it once Crush can show unprompted turns.
		defer close(l.drained)
		for {
			select {
			case <-l.idle:
				return
			case _, ok := <-l.lines:
				if !ok {
					l.dead.Store(true)
					<-l.idle
					return
				}
			}
		}
	}()
	l.timer = time.AfterFunc(claudeIdle, func() {
		claudeSessions.mu.Lock()
		if claudeSessions.m[sessionID] != l {
			claudeSessions.mu.Unlock()
			return
		}
		delete(claudeSessions.m, sessionID)
		claudeSessions.mu.Unlock()
		close(l.idle)
		<-l.drained
		l.p.finish()
	})
	claudeSessions.mu.Lock()
	old := claudeSessions.m[sessionID]
	claudeSessions.m[sessionID] = l
	claudeSessions.mu.Unlock()
	if old != nil {
		old.timer.Stop()
		close(old.idle)
		<-old.drained
		old.p.finish()
	}
}

func startClaude(m *Model, t Turn, key claudeKey) (*claudeLive, error) {
	args := []string{
		"-p", "--input-format", "stream-json", "--output-format", "stream-json",
		"--verbose", "--include-partial-messages", "--permission-prompt-tool", "stdio",
		// Echoes each user message as the model takes it in, which is how
		// steered messages are confirmed.
		"--replay-user-messages",
		"--model", m.ID,
	}
	if key.effort != "" {
		args = append(args, "--effort", key.effort)
	}
	if t.Resume != "" {
		args = append(args, "--resume", t.Resume)
	}
	if m.Guarded && !t.NoTools {
		// Edits need no approval under Crush's YOLO rules either; commands
		// still come to Crush to be checked.
		args = append(args, "--permission-mode", "acceptEdits")
	}
	if key.bypass {
		// Crush would approve every call anyway. Asking also changes how
		// Claude works: it splits jobs into more tool calls, each costing a
		// model round.
		args = append(args, "--permission-mode", "bypassPermissions")
	}
	// Wait for MCP servers before the first request, as `claude -p` with a
	// prompt argument does. Streamed input doesn't by default, so the first
	// prompt can't use their tools and their late arrival adds thousands of
	// tokens to the turn.
	env := append([]string{"MCP_CONNECTION_NONBLOCKING=0"}, t.Env...)
	if t.Instructions != "" {
		args = append(args, "--append-system-prompt", t.Instructions)
	}
	if !t.NoTools {
		// Main agents ask the user through `crush ask` and sub-agents
		// through their report; Claude's own question tool reaches no one.
		args = append(args, "--disallowedTools=AskUserQuestion")
	}
	if t.NoTools {
		args = append(args, "--tools", "", "--no-session-persistence", "--strict-mcp-config", "--safe-mode")
		if t.System != "" {
			args = append(args, "--system-prompt", t.System)
		}
		env = nil
	}
	p, err := startProcEnv(m.Dir, env, "claude", args...)
	if err != nil {
		return nil, err
	}
	// A CLI that dies right away fails this write; the read loop then
	// reports why.
	_ = p.send(map[string]any{"type": "control_request", "request_id": "crush-init", "request": map[string]any{"subtype": "initialize"}})
	return &claudeLive{p: p, lines: p.readLines(), key: key}, nil
}

func runClaude(ctx context.Context, m *Model, t Turn) error {
	key := claudeKey{dir: m.Dir, model: m.ID, effort: t.Effort, bypass: !t.NoTools && m.autoApproved(t.SessionID)}
	// A sub-agent runs one turn, so its process isn't kept for more.
	keep := !t.NoTools && t.SessionID != "" && !m.Guarded
	var live *claudeLive
	if keep {
		live = takeClaude(t.SessionID, key, t.Resume)
	}
	fresh := live == nil
	if fresh {
		var err error
		if live, err = startClaude(m, t, key); err != nil {
			return err
		}
	}
	p := live.p
	finished := false
	defer func() {
		if finished && keep && live.native != "" {
			keepClaude(t.SessionID, live)
		} else {
			p.finish()
		}
	}()
	stop := p.watchCancel(ctx, func() {
		_ = p.send(map[string]any{"type": "control_request", "request_id": "crush-interrupt", "request": map[string]any{"subtype": "interrupt"}})
	})
	defer stop()

	_ = p.send(claudeUserMessage(t.Prompt))

	// Claude takes messages written mid-turn in at its next tool result, or
	// runs them right after the turn if it was already answering.
	var sent steered
	stopSteer := pollSteer(t, func() bool { return true }, func(text string) {
		sent.add(text)
		if p.send(claudeUserMessage(text)) != nil {
			sent.take(text)
		}
	})
	defer stopSteer()

	// Crush tool name and input per tool_use ID, for results and approvals.
	calls := map[string][2]string{}
	started := false
	// Armed while waiting on steered messages after a result; Claude
	// doesn't echo every message it's given, and an unconfirmed one must
	// not hold the turn open forever. Crush runs it next instead.
	var steerWait <-chan time.Time
read:
	for {
		var raw []byte
		select {
		case r, ok := <-live.lines:
			if !ok {
				break read
			}
			raw, steerWait = r, nil
		case <-steerWait:
			finished = true
			return nil
		}
		var line claudeLine
		if json.Unmarshal(raw, &line) != nil {
			continue
		}
		switch line.Type {
		case "system":
			// Claude reports this again for each prompt a live process takes.
			if line.Subtype == "init" && line.SessionID != "" {
				started = true
				live.native = line.SessionID
				if err := t.Emit(Event{Type: EventSession, Session: line.SessionID}); err != nil {
					return err
				}
			}

		case "rate_limit_event":
			setLimits(config.TypeClaudeCode, claudeRateLimit(raw))

		case "stream_event":
			if err := claudeStreamEvent(line.Event, t.Emit); err != nil {
				return err
			}

		case "assistant":
			for _, b := range claudeBlocks(line.Message) {
				if b.Type != "tool_use" {
					continue
				}
				name, input := claudeTool(b.Name, b.Input)
				calls[b.ID] = [2]string{name, input}
				if err := t.Emit(Event{Type: EventToolCall, ID: b.ID, Name: name, Input: input}); err != nil {
					return err
				}
			}

		case "user":
			if line.IsReplay {
				if text := claudeUserText(line.Message); sent.take(text) {
					if err := t.Emit(Event{Type: EventUserMessage, Text: text}); err != nil {
						return err
					}
				}
				continue
			}
			for _, b := range claudeBlocks(line.Message) {
				if b.Type != "tool_result" {
					continue
				}
				call := calls[b.ToolUseID]
				out := claudeResultText(b.Content)
				meta := ""
				if !b.IsError {
					meta = resultMetadata(call[0], call[1], out)
				}
				if err := t.Emit(Event{Type: EventToolResult, ID: b.ToolUseID, Name: call[0], Output: out, Metadata: meta, IsError: b.IsError}); err != nil {
					return err
				}
			}

		case "control_request":
			go claudeControl(ctx, m, t, p, line)

		case "result":
			// A background task finishing reports its own result outside
			// the prompt; it does not end this turn.
			if line.Origin != nil && line.Origin.Kind == "task-notification" {
				continue
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if line.IsError || (line.Subtype != "" && line.Subtype != "success") {
				msg := line.Result
				if msg == "" {
					msg = strings.Join(line.Errors, "; ")
				}
				return errors.New(strings.TrimSpace("Claude Code: " + msg))
			}
			// A message steered in as the model finished runs as a
			// follow-up; wait for that one too.
			stopSteer()
			if sent.pending() > 0 {
				steerWait = time.After(steerEchoTimeout)
				continue
			}
			finished = true
			return nil
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if fresh && !started && t.Resume != "" {
		return ErrResume
	}
	return exitError("claude", p)
}

// steerEchoTimeout is how long a finished turn waits for Claude to start on
// a steered message.
var steerEchoTimeout = 15 * time.Second

func claudeUserMessage(text string) map[string]any {
	return map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": text}}
}

// claudeUserText returns the text of a replayed user message.
func claudeUserText(msg *claudeMessage) string {
	if msg == nil {
		return ""
	}
	var text string
	if json.Unmarshal(msg.Content, &text) == nil {
		return text
	}
	var sb strings.Builder
	for _, b := range claudeBlocks(msg) {
		if b.Type == "text" {
			sb.WriteString(b.Text)
		}
	}
	return sb.String()
}

func claudeStreamEvent(ev *claudeEvent, emit func(Event) error) error {
	if ev == nil {
		return nil
	}
	switch ev.Type {
	case "content_block_start":
		if ev.ContentBlock.Type == "text" {
			return emit(Event{Type: EventText, Text: TextBreak})
		}
		if ev.ContentBlock.Type == "tool_use" {
			name, _ := claudeTool(ev.ContentBlock.Name, nil)
			return emit(Event{Type: EventToolStart, ID: ev.ContentBlock.ID, Name: name})
		}
	case "content_block_delta":
		switch ev.Delta.Type {
		case "text_delta":
			return emit(Event{Type: EventText, Text: ev.Delta.Text})
		case "thinking_delta":
			if ev.Delta.Thinking != "" {
				return emit(Event{Type: EventReasoning, Text: ev.Delta.Thinking})
			}
		}
	case "message_delta":
		if u := ev.Usage; u != nil {
			return emit(Event{Type: EventUsage, Usage: fantasy.Usage{
				InputTokens:         u.InputTokens,
				OutputTokens:        u.OutputTokens,
				TotalTokens:         u.InputTokens + u.OutputTokens + u.CacheCreation + u.CacheRead,
				CacheCreationTokens: u.CacheCreation,
				CacheReadTokens:     u.CacheRead,
			}})
		}
	}
	return nil
}

// claudeControl answers a control request from the CLI. Tool permission
// prompts go to Crush's permission dialog; anything else is declined.
func claudeControl(ctx context.Context, m *Model, t Turn, p *proc, line claudeLine) {
	respond := func(resp map[string]any) {
		_ = p.send(map[string]any{"type": "control_response", "response": map[string]any{
			"subtype": "success", "request_id": line.RequestID, "response": resp,
		}})
	}
	if line.Request == nil || line.Request.Subtype != "can_use_tool" {
		_ = p.send(map[string]any{"type": "control_response", "response": map[string]any{
			"subtype": "error", "request_id": line.RequestID, "error": "not supported by Crush",
		}})
		return
	}
	req := line.Request
	name, input := claudeTool(req.ToolName, req.Input)
	if !t.NoTools && m.approve(ctx, t.SessionID, req.ToolUseID, name, input) {
		var in map[string]any
		_ = json.Unmarshal(req.Input, &in)
		respond(map[string]any{"behavior": "allow", "updatedInput": in})
		return
	}
	msg := "The user denied this tool call."
	if sleepRefused(name, input) {
		msg = tools.SleepRefusal
	}
	respond(map[string]any{"behavior": "deny", "message": msg})
}

func claudeBlocks(msg *claudeMessage) []claudeBlock {
	if msg == nil {
		return nil
	}
	var blocks []claudeBlock
	_ = json.Unmarshal(msg.Content, &blocks) // plain string content has no blocks
	return blocks
}

// claudeResultText flattens tool_result content (a string or text blocks).
func claudeResultText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var blocks []claudeBlock
	_ = json.Unmarshal(raw, &blocks)
	var parts []string
	for _, b := range blocks {
		if b.Type == "text" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "\n")
}

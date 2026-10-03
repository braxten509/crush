package cliagent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/secretguard"
)

// Claude Code's stream-json protocol, as used by its Agent SDKs: Messages
// API stream events wrapped in session events, plus control requests for
// tool permissions. See `claude --help` (--input-format/--output-format).
type claudeLine struct {
	Type      string                 `json:"type"`
	Subtype   string                 `json:"subtype"`
	Status    string                 `json:"status"`
	SessionID string                 `json:"session_id"`
	Usage     *claudeUsage           `json:"usage"`
	Event     *claudeEvent           `json:"event"`
	Message   *claudeMessage         `json:"message"`
	RequestID string                 `json:"request_id"`
	Request   *claudeRequest         `json:"request"`
	IsError   bool                   `json:"is_error"`
	Result    string                 `json:"result"`
	Errors    []string               `json:"errors"`
	Origin    *struct{ Kind string } `json:"origin"`
	IsReplay  bool                   `json:"isReplay"`
	// Indexes into Message.Content, supplied by Claude Code for user-facing
	// narration carried in thinking blocks. Unlisted blocks are reasoning.
	NarrationBlockIndexes []int `json:"narration_block_indexes"`
	// Set on lines from a sub-agent Claude runs itself.
	ParentToolUseID string `json:"parent_tool_use_id"`
	// task_started: the tool call a foreground task runs for.
	ToolUseID string `json:"tool_use_id"`
	// task_started and task_notification (the task ended): which task.
	TaskID         string `json:"task_id"`
	IsBackgrounded bool   `json:"is_backgrounded"`
	// On a tool result: set when the command went on in the background.
	ToolUseResult json.RawMessage `json:"tool_use_result"`
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
	Usage   *claudeUsage   `json:"usage"`
	Message *claudeMessage `json:"message"`
}

type claudeMessage struct {
	Content json.RawMessage `json:"content"`
	Usage   *claudeUsage    `json:"usage"`
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
	Thinking  string          `json:"thinking"`
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
var claudeIdle = 15 * time.Minute

// claudeKey is what a live Claude process was started with; a turn that
// needs anything else starts a new one.
type claudeKey struct {
	dir, model, effort string
	bypass, fast       bool
	ultracode          bool
}

// claudeLive is a Claude process kept open between the turns of one Crush
// session, as the interactive CLI does, so follow-ups skip its startup
// (settings, hooks, MCP servers).
type claudeLive struct {
	p       *proc
	lines   <-chan []byte
	key     claudeKey
	native  string
	pending [][]byte      // a reply Claude started between turns, unread
	idle    chan struct{} // closed to end the between-turns drain
	drained chan struct{}
	dead    atomic.Bool
	timer   *time.Timer
	// Tasks Claude has started and not reported ended, by task ID. Exiting
	// would stop the background ones and lose their results.
	tasksMu sync.Mutex
	tasks   map[string]bool
}

// track notes the tasks a line starts or ends.
func (l *claudeLive) track(line claudeLine) {
	if line.Type != "system" || line.TaskID == "" {
		return
	}
	l.tasksMu.Lock()
	defer l.tasksMu.Unlock()
	switch line.Subtype {
	case "task_started":
		if l.tasks == nil {
			l.tasks = map[string]bool{}
		}
		l.tasks[line.TaskID] = true
	case "task_notification":
		delete(l.tasks, line.TaskID)
	}
}

// busy reports whether a task Claude started is still running.
func (l *claudeLive) busy() bool {
	l.tasksMu.Lock()
	defer l.tasksMu.Unlock()
	return len(l.tasks) > 0
}

type claudeTurn struct {
	p *proc
	// ctrlB is set by a Ctrl+B that may have come before Claude started
	// the command; the command is backgrounded once it does.
	ctrlB atomic.Bool
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
// prompt, or closes it after [claudeIdle] with no task running.
func keepClaude(sessionID string, l *claudeLive) {
	l.idle, l.drained = make(chan struct{}), make(chan struct{})
	go func() {
		// Claude replies on its own when a background task finishes. The
		// reply is left unread for a Crush turn to show (OnUnprompted);
		// the status lines before it are dropped.
		defer close(l.drained)
		for {
			select {
			case <-l.idle:
				return
			case raw, ok := <-l.lines:
				if !ok {
					l.dead.Store(true)
					<-l.idle
					return
				}
				var line claudeLine
				if json.Unmarshal(raw, &line) != nil {
					continue
				}
				l.track(line)
				if line.Type == "system" && line.Subtype == "init" {
					l.pending = [][]byte{raw}
					go notifyUnprompted(sessionID)
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
		if l.busy() && !l.dead.Load() {
			l.timer.Reset(claudeIdle) // Claude reports the task when it ends
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
		// Crush's Claude processes are headless. Override the user's Chrome
		// integration opt-in so background agents never open browser tabs.
		"--no-chrome",
		// Echoes each user message as the model takes it in, which is how
		// steered messages are confirmed.
		"--replay-user-messages",
		"--model", m.ID,
	}
	if key.effort != "" {
		args = append(args, "--effort", key.effort)
	}
	// Claude Code has no flags for these; headless runs opt in through
	// settings. Its hooks from other settings files still run beside
	// Crush's secrets guard.
	settings := map[string]any{"hooks": map[string]any{"PreToolUse": []any{map[string]any{
		"matcher": "*",
		"hooks":   []any{map[string]any{"type": "command", "command": secretguard.HookCommand(), "timeout": 10}},
	}}}}
	if key.fast {
		settings["fastMode"] = true
	}
	if key.ultracode {
		settings["ultracode"] = true
	}
	b, _ := json.Marshal(settings)
	args = append(args, "--settings", string(b))
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
	// Disable native memory only in Crush's child process, including helpers.
	// Crush supplies the shared memory index and rules through Instructions.
	env = append(env, "CLAUDE_CODE_DISABLE_AUTO_MEMORY=1", "CLAUDE_CODE_DISABLE_ORG_MEMORY=1")
	env = append(env, "CLAUDE_CODE_DISABLE_CLAUDE_MDS=1")
	p, err := startReviewProc(m.Dir, env, !t.NoTools, "claude", args...)
	if err != nil {
		return nil, err
	}
	// A CLI that dies right away fails this write; the read loop then
	// reports why.
	_ = p.send(map[string]any{"type": "control_request", "request_id": "crush-init", "request": map[string]any{"subtype": "initialize"}})
	return &claudeLive{p: p, lines: p.readLines(), key: key}, nil
}

func runClaude(ctx context.Context, m *Model, t Turn) error {
	key := claudeKey{dir: m.Dir, model: m.ID, effort: t.Effort, fast: m.ServiceTier == "fast", ultracode: m.Ultracode, bypass: !t.NoTools && m.autoApproved(t.SessionID)}
	// A sub-agent runs one turn, so its process isn't kept for more.
	keep := !t.NoTools && t.SessionID != "" && !m.Guarded
	var live *claudeLive
	if keep {
		live = takeClaude(t.SessionID, key, t.Resume)
	}
	fresh := live == nil
	if fresh && t.Continue {
		return nil // the process that replied is gone
	}
	if fresh {
		var err error
		if live, err = startClaude(m, t, key); err != nil {
			return err
		}
	}
	p := live.p
	t.Emit = p.reviewEvents(t.Emit)
	finished := false
	ctl := &claudeTurn{p: p}
	if t.SessionID != "" && !t.NoTools {
		defer onBackground(t.SessionID, func() bool {
			ctl.ctrlB.Store(true)
			return p.send(map[string]any{"type": "control_request", "request_id": "crush-ctrl-b", "request": map[string]any{"subtype": "background_tasks"}}) == nil
		})()
	}
	defer func() {
		if finished && keep && live.native != "" {
			keepClaude(t.SessionID, live)
		} else {
			p.finish()
		}
	}()
	interrupt := func() {
		_ = p.send(map[string]any{"type": "control_request", "request_id": "crush-interrupt", "request": map[string]any{"subtype": "interrupt"}})
	}
	stop := p.watchCancel(ctx, interrupt)
	defer stop()

	pending := live.pending
	live.pending = nil
	// Results before Claude takes in this prompt end a reply it started on
	// its own; the prompt's own result may carry a background notice's
	// origin when one was folded into it.
	promptTaken := t.Continue
	if t.Continue {
		if len(pending) == 0 {
			finished = true
			return nil // already shown by another turn
		}
	} else {
		_ = p.send(claudePrompt(withSavedImagePaths(t.Prompt, t.Attachments), t.Attachments))
	}

	// Claude takes messages written mid-turn in at its next tool result, or
	// runs them right after the turn if it was already answering.
	var sent steered
	startSteer := func() func() {
		return pollSteerInput(t, func() bool { return true }, func(text string, attachments []message.Attachment) {
			sent.add(text)
			prompt := claudePrompt(withSavedImagePaths(text, attachments), attachments)
			// Explicitly fold this input in at the next tool boundary. Claude's
			// default priority can be "later", which waits for the whole turn.
			prompt["priority"] = "next"
			if p.send(prompt) != nil {
				sent.take(text)
			}
		})
	}
	stopSteer := startSteer()
	defer func() { stopSteer() }()

	// Crush tool name and input per tool_use ID, for results and approvals.
	calls := map[string][2]string{}
	// Classification arrives with the completed assistant block, after its
	// thinking deltas. Hold those deltas so narration never enters reasoning.
	var thinking strings.Builder
	flushThinking := func(narration bool) error {
		text := thinking.String()
		thinking.Reset()
		if text == "" {
			return nil
		}
		kind := EventReasoning
		if narration {
			kind = EventText
			if err := t.Emit(Event{Type: EventText, Text: TextBreak}); err != nil {
				return err
			}
		}
		return t.Emit(Event{Type: kind, Text: text})
	}
	recordUsage := claudeUsageRecorder(t.Emit)
	started := false
	// Armed while waiting on steered messages after a result; Claude
	// doesn't echo every message it's given, and an unconfirmed one must
	// not hold the turn open forever. Crush runs it next instead.
	var steerWait <-chan time.Time
	interrupted := 0 // results since the turn was interrupted
read:
	for {
		var raw []byte
		if len(pending) > 0 {
			raw, pending = pending[0], pending[1:]
		} else {
			select {
			case r, ok := <-live.lines:
				if !ok {
					break read
				}
				raw = r
			case <-steerWait:
				finished = true
				return nil
			}
		}
		var line claudeLine
		if json.Unmarshal(raw, &line) != nil {
			continue
		}
		live.track(line)
		if line.ParentToolUseID != "" {
			continue // a sub-agent's own steps; its result comes as a tool result
		}
		if ctx.Err() != nil {
			// Interrupted. Claude runs messages steered in but not yet
			// taken right after, so that turn is stopped too. The process
			// is then kept: exiting would stop its background tasks.
			stopSteer()
			switch {
			case line.Type == "user" && line.IsReplay:
				sent.take(withoutImagePaths(claudeUserText(line.Message)))
			case line.Type == "system" && line.Subtype == "init" && interrupted > 0:
				interrupt()
			case line.Type == "result":
				interrupted++
				if sent.pending() == 0 || interrupted > 1 {
					finished = true
					return ctx.Err()
				}
			}
			continue
		}
		switch line.Type {
		case "system":
			if line.Subtype == "status" || line.Subtype == "compact_boundary" {
				if err := t.Emit(Event{Type: EventCompacting, Compacting: line.Status == "compacting"}); err != nil {
					return err
				}
			}
			// Only a started task can be backgrounded; asking earlier
			// finds nothing.
			if line.Subtype == "task_started" && line.ToolUseID != "" && !line.IsBackgrounded {
				if ctl.ctrlB.Swap(false) {
					_ = p.send(map[string]any{"type": "control_request", "request_id": "crush-ctrl-b-" + line.ToolUseID, "request": map[string]any{"subtype": "background_tasks", "tool_use_id": line.ToolUseID}})
				}
			}
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
			if ev := line.Event; ev != nil && (ev.Type == "message_start" || ev.Type == "content_block_start" || ev.Type == "content_block_delta") {
				steerWait = nil
			}
			if err := recordUsage(line); err != nil {
				return err
			}
			if ev := line.Event; ev != nil && ev.Type == "content_block_delta" && ev.Delta.Type == "thinking_delta" {
				thinking.WriteString(ev.Delta.Thinking)
				continue
			}
			if err := claudeStreamEvent(line.Event, t.Emit); err != nil {
				return err
			}

		case "assistant":
			steerWait = nil
			if err := recordUsage(line); err != nil {
				return err
			}
			for index, b := range claudeBlocks(line.Message) {
				if b.Type == "thinking" {
					thinking.Reset()
					thinking.WriteString(b.Thinking)
					if err := flushThinking(slices.Contains(line.NarrationBlockIndexes, index)); err != nil {
						return err
					}
					continue
				}
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
				if text := withoutImagePaths(claudeUserText(line.Message)); sent.take(text) {
					steerWait = nil
					if err := t.Emit(Event{Type: EventUserMessage, Text: text}); err != nil {
						return err
					}
				} else {
					promptTaken = true
				}
				continue
			}
			for _, b := range claudeBlocks(line.Message) {
				if b.Type != "tool_result" {
					continue
				}
				call := calls[b.ToolUseID]
				ctl.ctrlB.Store(false) // the command it was for is done
				out := claudeResultText(b.Content)
				meta := ""
				if !b.IsError {
					meta = resultMetadata(call[0], call[1], out)
					var res struct {
						BackgroundTaskID string `json:"backgroundTaskId"`
					}
					if json.Unmarshal(line.ToolUseResult, &res) == nil && res.BackgroundTaskID != "" {
						meta = markBackground(call[0], meta)
					}
				}
				if err := t.Emit(Event{Type: EventToolResult, ID: b.ToolUseID, Name: call[0], Output: out, Metadata: meta, IsError: b.IsError}); err != nil {
					return err
				}
			}

		case "control_request":
			go claudeControl(ctx, m, t, p, line)

		case "result":
			if err := flushThinking(false); err != nil {
				return err
			}
			// A background task finishing reports its own result outside
			// the prompt; it does not end this turn.
			if ctx.Err() != nil {
				return ctx.Err()
			}
			notice := line.Origin != nil && line.Origin.Kind == "task-notification"
			if !promptTaken && notice {
				continue // a reply Claude started on its own before this prompt
			}
			promptTaken = true
			if err := recordUsage(line); err != nil {
				return err
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
				// The follow-up is still part of this Crush turn. Keep
				// forwarding new prompts while Claude works on it.
				stopSteer = startSteer()
				steerWait = time.After(steerEchoTimeout)
				continue
			}
			finished = true
			return nil
		}
	}
	if err := flushThinking(false); err != nil {
		return err
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

// claudePrompt is the turn's user message, with its images inline.
func claudePrompt(text string, attachments []message.Attachment) map[string]any {
	msg := claudeUserMessage(text)
	var content []any
	if text != "" {
		// The API rejects empty text blocks, as in an image-only message.
		content = append(content, map[string]any{"type": "text", "text": text})
	}
	images := 0
	for _, a := range attachments {
		if a.IsImage() {
			content = append(content, map[string]any{"type": "image", "source": map[string]any{
				"type": "base64", "media_type": a.MimeType, "data": base64.StdEncoding.EncodeToString(a.Content),
			}})
			images++
		}
	}
	if images > 0 {
		msg["message"].(map[string]any)["content"] = content
	}
	return msg
}

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

// claudeUsageRecorder emits each request once, merging cumulative stream
// updates and ignoring the turn-wide result when request usage was emitted.
func claudeUsageRecorder(emit func(Event) error) func(claudeLine) error {
	var usage *claudeUsage
	emitted, recorded, streaming := false, false, false
	merge := func(update *claudeUsage) {
		if update == nil {
			return
		}
		if usage == nil {
			usage = &claudeUsage{}
		}
		usage.InputTokens = max(usage.InputTokens, update.InputTokens)
		usage.OutputTokens = max(usage.OutputTokens, update.OutputTokens)
		usage.CacheCreation = max(usage.CacheCreation, update.CacheCreation)
		usage.CacheRead = max(usage.CacheRead, update.CacheRead)
	}
	flush := func() error {
		if usage == nil || emitted {
			return nil
		}
		emitted, recorded = true, true
		return emit(Event{Type: EventUsage, Usage: fantasy.Usage{
			InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens,
			TotalTokens:         usage.InputTokens + usage.OutputTokens + usage.CacheCreation + usage.CacheRead,
			CacheCreationTokens: usage.CacheCreation, CacheReadTokens: usage.CacheRead,
		}})
	}
	return func(line claudeLine) error {
		switch line.Type {
		case "stream_event":
			if ev := line.Event; ev != nil {
				switch ev.Type {
				case "message_start":
					if err := flush(); err != nil {
						return err
					}
					usage, emitted, streaming = nil, false, true
					if ev.Message != nil {
						merge(ev.Message.Usage)
					}
				case "message_delta":
					streaming = true
					merge(ev.Usage)
				case "message_stop":
					return flush()
				}
			}
		case "assistant":
			if line.Message != nil {
				if !streaming {
					usage, emitted = nil, false
				}
				merge(line.Message.Usage)
				return flush()
			}
		case "result":
			if !recorded {
				merge(line.Usage)
			}
			if err := flush(); err != nil {
				return err
			}
			usage, emitted, recorded, streaming = nil, false, false, false
		}
		return nil
	}
}

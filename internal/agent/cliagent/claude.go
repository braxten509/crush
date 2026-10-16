package cliagent

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/crush/internal/secretguard"
)

// Claude Code's stream-json protocol, as used by its Agent SDKs: Messages
// API stream events wrapped in session events, plus control requests for
// tool permissions. See `claude --help` (--input-format/--output-format).
type claudeLine struct {
	ClaudeCodeVersion string                 `json:"claude_code_version"`
	Type              string                 `json:"type"`
	Subtype           string                 `json:"subtype"`
	Status            string                 `json:"status"`
	SessionID         string                 `json:"session_id"`
	Usage             *claudeUsage           `json:"usage"`
	Event             *claudeEvent           `json:"event"`
	Message           *claudeMessage         `json:"message"`
	RequestID         string                 `json:"request_id"`
	Response          json.RawMessage        `json:"response"`
	Request           *claudeRequest         `json:"request"`
	IsError           bool                   `json:"is_error"`
	Result            string                 `json:"result"`
	Errors            []string               `json:"errors"`
	Origin            *struct{ Kind string } `json:"origin"`
	IsReplay          bool                   `json:"isReplay"`
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
	liveProc[claudeKey]
	native  string
	pending [][]byte // a reply Claude started between turns, unread
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

func (l *claudeLive) resumeID() string { return l.native }

// claudeSessions holds the live processes, one per Crush session.
var claudeSessions = newLivePool[claudeKey, *claudeLive]()

// keepClaude parks a process after a finished turn until the session's next
// prompt, or closes it after [claudeIdle] with no task running.
func keepClaude(sessionID string, l *claudeLive) {
	// Claude replies on its own when a background task finishes. The reply
	// is left unread for a Crush turn to show (OnUnprompted); the status
	// lines before it are dropped.
	claudeSessions.keep(sessionID, l, func(raw []byte) bool {
		var line claudeLine
		if json.Unmarshal(raw, &line) != nil {
			return false
		}
		l.track(line)
		if line.Type == "system" && line.Subtype == "init" {
			l.pending = [][]byte{raw}
			go notifyUnprompted(sessionID)
			return true
		}
		return false
	}, l.busy)
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
		// Newer models omit thinking text unless asked for summaries,
		// which the busy status shows a line of. The chat still hides it.
		"--thinking-display", "summarized",
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
	// Claude leaves its to-do list out for newer models unless asked; Crush
	// shows it as the chat's to-do list. TodoWrite, which sends the whole
	// list each time, rather than the step-by-step Task tools.
	env = append(env, "CLAUDE_CODE_ENABLE_TODO_TOOLS=1", "CLAUDE_CODE_ENABLE_TASKS=0")
	p, err := m.startTurnProc(t.NoTools, env, "claude", args...)
	if err != nil {
		return nil, err
	}
	// A CLI that dies right away fails this write; the read loop then
	// reports why.
	_ = p.send(map[string]any{"type": "control_request", "request_id": "crush-init", "request": map[string]any{"subtype": "initialize"}})
	return &claudeLive{liveProc: liveProc[claudeKey]{p: p, lines: p.readLines(), key: key}}, nil
}

// claudeRun is one turn of a Claude Code process.
type claudeRun struct {
	ctx  context.Context
	m    *Model
	t    Turn
	p    *proc
	live *claudeLive
	ctl  *claudeTurn
	// finished is set once the turn ended normally, so the process may be
	// kept.
	finished bool
	// promptTaken is set once Claude took in this turn's prompt. Results
	// before it end a reply it started on its own; the prompt's own result
	// may carry a background notice's origin when one was folded into it.
	promptTaken bool
	started     bool

	// Claude takes messages written mid-turn in at its next tool result, or
	// runs them right after the turn if it was already answering.
	sent      steered
	stopSteer func()
	// steerWait is armed while waiting on steered messages after a result;
	// Claude doesn't echo every message it's given, and an unconfirmed one
	// must not hold the turn open forever. Crush runs it next instead.
	steerWait <-chan time.Time
	// interrupted counts results since the turn was interrupted.
	interrupted int

	// calls holds the Crush tool name and input per tool_use ID, for
	// results and approvals.
	calls map[string][2]string
	// Classification arrives with the completed assistant block, after its
	// thinking deltas. Hold those deltas so narration never enters reasoning.
	thinking    strings.Builder
	recordUsage func(claudeLine) error

	lastContextRequest time.Time
	contextSupported   bool
}

func runClaude(ctx context.Context, m *Model, t Turn) error {
	key := claudeKey{dir: m.Dir, model: m.ID, effort: t.Effort, fast: m.ServiceTier == "fast", ultracode: m.Ultracode, bypass: !t.NoTools && m.autoApproved(t.SessionID)}
	// A sub-agent runs one turn, so its process isn't kept for more.
	keep := !t.NoTools && t.SessionID != "" && !m.Guarded
	var live *claudeLive
	if keep {
		live = claudeSessions.take(t.SessionID, key, t.Resume)
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
	r := &claudeRun{ctx: ctx, m: m, t: t, p: p, live: live, ctl: &claudeTurn{p: p}, calls: map[string][2]string{}}
	if t.SessionID != "" && !t.NoTools {
		defer onBackground(t.SessionID, r.background)()
	}
	defer func() {
		if r.finished && keep && live.native != "" {
			keepClaude(t.SessionID, live)
		} else {
			p.finish()
		}
	}()
	stop := p.watchCancel(ctx, r.interrupt)
	defer stop()

	pending := live.pending
	live.pending = nil
	r.promptTaken = t.Continue
	if t.Continue {
		if len(pending) == 0 {
			r.finished = true
			return nil // already shown by another turn
		}
	} else {
		_ = p.send(claudePrompt(withSavedImagePaths(t.Prompt, t.Attachments), t.Attachments))
	}

	r.stopSteer = r.startSteer()
	defer func() { r.stopSteer() }()

	r.recordUsage = claudeUsageRecorder(r.t.Emit)
	if done, err := r.readLoop(pending); done {
		return err
	}
	if err := r.flushThinking(false); err != nil {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if fresh && !r.started && t.Resume != "" {
		return ErrResume
	}
	return exitError("claude", p)
}

// background handles Ctrl+B by asking Claude Code to move its running
// tasks to the background.
func (r *claudeRun) background() bool {
	r.ctl.ctrlB.Store(true)
	return r.p.send(map[string]any{"type": "control_request", "request_id": "crush-ctrl-b", "request": map[string]any{"subtype": "background_tasks"}}) == nil
}

// readLoop handles lines left over from an earlier turn, then the process's
// output, until the turn ends. done reports that runClaude should return
// err; otherwise the process's output ended.
func (r *claudeRun) readLoop(pending [][]byte) (done bool, err error) {
	for {
		var raw []byte
		if len(pending) > 0 {
			raw, pending = pending[0], pending[1:]
		} else {
			select {
			case line, ok := <-r.live.lines:
				if !ok {
					return false, nil
				}
				raw = line
			case <-r.steerWait:
				r.finished = true
				return true, nil
			}
		}
		var line claudeLine
		if json.Unmarshal(raw, &line) != nil {
			continue
		}
		r.live.track(line)
		if line.ParentToolUseID != "" {
			continue // a sub-agent's own steps; its result comes as a tool result
		}
		var done bool
		var err error
		if r.ctx.Err() != nil {
			done, err = r.handleInterrupted(line)
		} else {
			done, err = r.handleLine(line, raw)
		}
		if done || err != nil {
			return true, err
		}
	}
}

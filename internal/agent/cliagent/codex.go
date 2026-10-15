package cliagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/secretguard"
	"github.com/charmbracelet/crush/internal/version"
)

// Codex is driven through `codex app-server`, its JSON-RPC protocol for rich
// clients (the one its IDE extension uses). `codex app-server generate-ts`
// prints the schema.

type codexItem struct {
	Type             string          `json:"type"`
	ID               string          `json:"id"`
	Phase            string          `json:"phase"`
	Command          string          `json:"command"`
	Cwd              string          `json:"cwd"`
	Status           string          `json:"status"`
	AggregatedOutput *string         `json:"aggregatedOutput"`
	ExitCode         *int            `json:"exitCode"`
	Changes          []codexChange   `json:"changes"`
	Server           string          `json:"server"`
	Tool             string          `json:"tool"`
	Arguments        json.RawMessage `json:"arguments"`
	Result           *struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"result"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
	Query   string `json:"query"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

type codexChange struct {
	Path string `json:"path"`
	Kind struct {
		Type string `json:"type"`
	} `json:"kind"`
	Diff string `json:"diff"`
}

type codexParams struct {
	Item       codexItem `json:"item"`
	ItemID     string    `json:"itemId"`
	Delta      string    `json:"delta"`
	Command    string    `json:"command"`
	TokenUsage *struct {
		ModelContextWindow int64 `json:"modelContextWindow"`
		Last               struct {
			InputTokens       int64 `json:"inputTokens"`
			CachedInputTokens int64 `json:"cachedInputTokens"`
			OutputTokens      int64 `json:"outputTokens"`
			ReasoningTokens   int64 `json:"reasoningOutputTokens"`
			TotalTokens       int64 `json:"totalTokens"`
		} `json:"last"`
	} `json:"tokenUsage"`
	Turn struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	} `json:"turn"`
}

const (
	codexInitID   = "1"
	codexThreadID = "2"
	codexTurnID   = "3"
	// Listing hooks and trusting Crush's secrets guard, between
	// initializing and starting the thread.
	codexHooksID = "hooks"
	codexTrustID = "hooks-trust"
)

// codexGuardHook adds Crush's secrets guard as a PreToolUse hook through a
// session flag, which Codex reads like its config.toml.
func codexGuardHook() string {
	return fmt.Sprintf(`hooks.PreToolUse=[{matcher=".*",hooks=[{type="command",command=%s,timeout=10,statusMessage="Checking secrets"}]}]`, strconv.Quote(secretguard.HookCommand()))
}

// codexGuardTrust returns the request that trusts Crush's secrets guard, or
// nil when it is trusted already. Codex skips new hooks until they are
// trusted, and it records trust by the hook's hash, so only this exact
// command is trusted.
func codexGuardTrust(result json.RawMessage) map[string]any {
	var res struct {
		Data []struct {
			Hooks []struct {
				Key         string `json:"key"`
				Command     string `json:"command"`
				Source      string `json:"source"`
				CurrentHash string `json:"currentHash"`
				TrustStatus string `json:"trustStatus"`
			} `json:"hooks"`
		} `json:"data"`
	}
	_ = json.Unmarshal(result, &res)
	for _, d := range res.Data {
		for _, h := range d.Hooks {
			if h.Source == "sessionFlags" && h.Command == secretguard.HookCommand() && h.TrustStatus != "trusted" && h.CurrentHash != "" {
				return map[string]any{"id": codexTrustID, "method": "config/value/write", "params": map[string]any{
					"keyPath":       fmt.Sprintf("hooks.state.%s.trusted_hash", strconv.Quote(h.Key)),
					"value":         h.CurrentHash,
					"mergeStrategy": "replace",
				}}
			}
		}
	}
	return nil
}

// codexKey is what a live Codex process's thread was started with; a turn
// that needs anything else starts a new one.
type codexKey struct {
	dir, model, tier, approval, sandbox string
}

// codexLive is a Codex process kept open between the turns of one Crush
// session, so commands it moved to the background keep running and the
// model can check on them, and follow-ups skip its startup.
type codexLive struct {
	liveProc[codexKey]
	thread string
}

func (l *codexLive) resumeID() string { return l.thread }

// codexSessions holds the live processes, one per Crush session.
// ponytail: a turn with other settings closes one, stopping any commands it
// moved to the background; so does [claudeIdle] without a turn.
var codexSessions = newLivePool[codexKey, *codexLive]()

// codexCtrlB is what Codex is told after Ctrl+B interrupted its turn; the
// interrupt kept it from learning the commands' terminal sessions.
func codexCtrlB(sessions []string) string {
	where := "in its terminal session"
	if len(sessions) > 0 {
		where = "in terminal session " + strings.Join(sessions, ", ") + " (poll it with write_stdin and empty input)"
	}
	return "The user pressed Ctrl+B to move the command you were waiting on to the background. It is still running " + where + ", so don't start it again and don't wait for it now: carry on with anything else, or say in a sentence that it's running in the background and end your turn. Check on it later, e.g. when the user asks."
}

// codexRun is one turn of a Codex app-server process: what it was started
// with, the thread and turn it drives, and what it has reported so far.
type codexRun struct {
	ctx context.Context
	m   *Model
	t   Turn
	p   *proc

	approval, sandbox string
	threadID          string
	// active holds the thread and turn IDs, read by the cancel watcher.
	active atomic.Pointer[[2]string]
	// finished is set once the turn completed, so the process may be kept.
	finished bool

	// Messages queued mid-turn go in through turn/steer; Codex reports each
	// as a userMessage item when the model takes it in.
	sent      steered
	steers    int
	stopSteer func()

	contextSettings                               codexContextSettings
	catalogWindow, catalogLimit, effectivePercent int64
	reportedWindow                                int64
	contextRequested                              bool
	compacting                                    bool
	lastActivity                                  time.Time
	// cue says what the model is likely doing once Codex goes quiet.
	// Codex reports a tool call only once the model has written all of
	// it, which can take a minute for a long patch.
	cue writingCue

	// Ctrl+B interrupts the turn: Codex keeps its commands running in
	// their terminal sessions, and a new turn tells the model so.
	running          atomic.Int32 // commands in progress
	ctrlB, movedToBg atomic.Bool
	openCmds         map[string]time.Time
	backgrounded     map[string]bool // their results are already shown

	// Crush tool name and input per item (file changes expand to one call
	// per file), and command output streamed so far.
	calls    map[string][2]string
	changes  map[string][]codexChange
	output   map[string]*strings.Builder
	textOpen bool
}

func runCodex(ctx context.Context, m *Model, t Turn) error {
	r := newCodexRun(ctx, m, t)
	key := codexKey{dir: m.Dir, model: m.ID, tier: m.ServiceTier, approval: r.approval, sandbox: r.sandbox}
	// A sub-agent runs one turn, so its process isn't kept for more.
	keep := !t.NoTools && t.SessionID != "" && !m.Guarded
	live, lines, err := r.open(keep, key)
	if err != nil {
		return err
	}
	p := r.p
	r.t.Emit = p.reviewEvents(r.t.Emit)
	r.catalogWindow, r.catalogLimit, r.effectivePercent = codexCatalogContext(m.ID)
	defer func() {
		if r.finished && keep && r.threadID != "" {
			codexSessions.keep(t.SessionID, &codexLive{liveProc: liveProc[codexKey]{p: p, lines: lines, key: key}, thread: r.threadID}, nil, nil)
		} else {
			p.finish()
		}
	}()

	stop := p.watchCancel(ctx, r.cancel)
	defer stop()

	r.stopSteer = pollSteerInputRetry(r.t, func() bool { return r.active.Load() != nil }, r.steer)
	defer r.stopSteer()

	if err := r.begin(live != nil); err != nil {
		return err
	}

	if t.SessionID != "" && !t.NoTools {
		defer onBackground(t.SessionID, r.background)()
	}

	if done, err := r.readLoop(lines); done {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if live != nil {
		return errors.New("Codex: the kept process ended")
	}
	return exitError("codex", p)
}

// newCodexRun prepares a Codex turn, picking the approval policy and
// sandbox from the session's permissions.
func newCodexRun(ctx context.Context, m *Model, t Turn) *codexRun {
	r := &codexRun{
		ctx: ctx, m: m, t: t,
		openCmds:     map[string]time.Time{},
		backgrounded: map[string]bool{},
		calls:        map[string][2]string{},
		changes:      map[string][]codexChange{},
		output:       map[string]*strings.Builder{},
	}
	// Crush's permission prompts replace Codex's own; the sandbox still
	// applies. YOLO mode lifts both, matching Crush's own tools.
	r.approval, r.sandbox = "on-request", "workspace-write"
	if t.NoTools || m.ReadOnly {
		r.approval, r.sandbox = "never", "read-only"
	} else if m.autoApproved(t.SessionID) {
		r.approval, r.sandbox = "never", "danger-full-access"
	}
	return r
}

// open resumes the session's kept Codex process when it matches, or starts
// a new app server. It returns the kept process, if any, and the lines the
// process prints.
func (r *codexRun) open(keep bool, key codexKey) (*codexLive, <-chan []byte, error) {
	var live *codexLive
	if keep {
		live = codexSessions.take(r.t.SessionID, key, r.t.Resume)
	}
	if live != nil {
		r.p, r.threadID = live.p, live.thread
		return live, live.lines, nil
	}
	// Crush hands every CLI the shared memory; Codex's own stays off.
	var err error
	args := []string{"app-server", "--disable", "memories", "-c", "project_doc_max_bytes=0", "-c", codexGuardHook()}
	if r.p, err = startReviewProc(r.m.Dir, r.t.Env, !r.t.NoTools, "codex", args...); err != nil {
		return nil, nil, err
	}
	return nil, r.p.readLines(), nil
}

// cancel interrupts the running turn, or closes the process's input when
// no turn has started yet.
func (r *codexRun) cancel() {
	if ids := r.active.Load(); ids != nil {
		_ = r.p.send(map[string]any{"id": "4", "method": "turn/interrupt", "params": map[string]any{"threadId": ids[0], "turnId": ids[1]}})
	} else {
		r.p.closeInput()
	}
}

// begin starts the turn on a kept process, or initializes a new one; the
// turn then starts once the thread is ready.
func (r *codexRun) begin(resumed bool) error {
	if resumed {
		if err := r.t.Emit(Event{Type: EventSession, Session: r.threadID}); err != nil {
			return err
		}
		r.startTurn(codexInput(withSavedImagePaths(r.t.Prompt, r.t.Attachments), r.t.Attachments))
		return nil
	}
	_ = r.p.send(map[string]any{"id": codexInitID, "method": "initialize", "params": map[string]any{
		"clientInfo": map[string]any{"name": "crush", "title": "Crush", "version": version.Version},
		// For thread/backgroundTerminals/list after Ctrl+B.
		"capabilities": map[string]any{"experimentalApi": true},
	}})
	return nil
}

// background handles Ctrl+B: it interrupts the turn while commands run so
// they carry on in the background. It reports whether a turn was running.
func (r *codexRun) background() bool {
	if r.active.Load() == nil {
		return false
	}
	r.ctrlB.Store(true)
	if r.running.Load() > 0 {
		r.interruptForBg()
	}
	return true
}

// writingCue is what an item that just ended says about the model's next
// output.
type writingCue int

const (
	cueNone writingCue = iota
	// cueNow follows a preamble ("I'll run the tests"): a tool call comes
	// next.
	cueNow
	// cueQuiet follows a chunk of thinking: the model is writing a tool
	// call unless another chunk arrives within codexWritingQuiet.
	cueQuiet
)

// codexWritingQuiet is how long Codex stays silent after a chunk of
// thinking before Crush says the model is writing. Thinking arrives in
// chunks at most about 11 seconds apart (measured on GPT-6.1 Sol).
var codexWritingQuiet = 15 * time.Second

// readLoop handles the process's messages until the turn ends. done reports
// that runCodex should return err; otherwise the process's output ended.
func (r *codexRun) readLoop(lines <-chan []byte) (done bool, err error) {
	quiet := time.NewTimer(codexWritingQuiet)
	quiet.Stop()
	defer quiet.Stop()
	for {
		var raw []byte
		select {
		case line, ok := <-lines:
			if !ok {
				return false, nil
			}
			raw = line
		case <-quiet.C:
			if r.ctx.Err() == nil {
				if err := r.emitWriting(); err != nil {
					return true, err
				}
			}
			continue
		}
		var msg rpcMessage
		if json.Unmarshal(raw, &msg) != nil {
			continue
		}
		id := strings.Trim(string(msg.ID), `"`)

		// Server-to-client requests: approvals. They block on the user, so
		// answer them off the read loop.
		if msg.Method != "" && id != "" {
			quiet.Stop()
			var params codexParams
			_ = json.Unmarshal(msg.Params, &params)
			go codexRequest(r.ctx, r.m, r.t, r.p, msg, params, r.changes[params.ItemID])
			continue
		}
		if r.ctx.Err() != nil {
			// Interrupted: once the turn winds down the process is kept, as
			// exiting would stop the commands it runs in the background.
			r.stopSteer()
			if msg.Method == "turn/completed" {
				r.finished = true
				return true, r.ctx.Err()
			}
			continue
		}

		var done bool
		var err error
		if msg.Method == "" {
			// Responses to our requests.
			err = r.handleResponse(id, msg)
		} else {
			done, err = r.handleNotification(msg)
		}
		if done || err != nil {
			return true, err
		}
		if strings.HasPrefix(msg.Method, "item/") {
			if err := r.followCue(quiet); err != nil {
				return true, err
			}
		}
	}
}

// followCue acts on what the item that just ended says comes next. Any
// other item means the model is visibly doing something, so the quiet timer
// stops.
func (r *codexRun) followCue(quiet *time.Timer) error {
	cue := r.cue
	r.cue = cueNone
	quiet.Stop()
	switch cue {
	case cueNow:
		return r.emitWriting()
	case cueQuiet:
		quiet.Reset(codexWritingQuiet)
	}
	return nil
}

func (r *codexRun) emitWriting() error {
	return r.t.Emit(Event{Type: EventActivity, Activity: ActivityWriting})
}

// steer sends a message queued mid-turn to the running turn.
func (r *codexRun) steer(text string, attachments []message.Attachment) bool {
	ids := r.active.Load()
	if ids == nil {
		return false
	}
	r.steers++
	r.sent.add(text)
	err := r.p.send(map[string]any{"id": "steer-" + strconv.Itoa(r.steers), "method": "turn/steer", "params": map[string]any{
		"threadId":       ids[0],
		"expectedTurnId": ids[1],
		"input":          codexInput(withSavedImagePaths(text, attachments), attachments),
	}})
	if err != nil {
		r.sent.take(text)
	}
	return true
}

func (r *codexRun) startTurn(input []any) {
	params := map[string]any{"threadId": r.threadID, "input": input}
	if r.t.Effort != "" {
		params["effort"] = r.t.Effort
	}
	if r.m.ServiceTier != "" {
		params["serviceTier"] = r.m.ServiceTier
	}
	_ = r.p.send(map[string]any{"id": codexTurnID, "method": "turn/start", "params": params})
}

func (r *codexRun) startThread() {
	params := map[string]any{"cwd": r.m.Dir, "model": r.m.ID, "approvalPolicy": r.approval, "sandbox": r.sandbox}
	if r.m.ServiceTier != "" {
		params["serviceTier"] = r.m.ServiceTier
	}
	method := "thread/start"
	if r.t.Resume != "" {
		method, params["threadId"] = "thread/resume", r.t.Resume
	} else if r.t.NoTools {
		params["ephemeral"] = true
	}
	_ = r.p.send(map[string]any{"id": codexThreadID, "method": method, "params": params})
}

func (r *codexRun) interruptForBg() {
	if ids := r.active.Load(); ids != nil && r.movedToBg.CompareAndSwap(false, true) {
		r.ctrlB.Store(false)
		_ = r.p.send(map[string]any{"id": "ctrl-b", "method": "turn/interrupt", "params": map[string]any{"threadId": ids[0], "turnId": ids[1]}})
	}
}

func (r *codexRun) reportContext() error {
	if r.t.NoTools {
		return nil
	}
	event := codexContextBudget(r.contextSettings, r.catalogWindow, r.catalogLimit, r.reportedWindow, r.effectivePercent)
	if event.ContextLimit <= 0 {
		return nil
	}
	return r.t.Emit(event)
}

func (r *codexRun) emitCall(id, name, input string) error {
	r.calls[id] = [2]string{name, input}
	r.textOpen = false
	if err := r.t.Emit(Event{Type: EventToolStart, ID: id, Name: name}); err != nil {
		return err
	}
	return r.t.Emit(Event{Type: EventToolCall, ID: id, Name: name, Input: input})
}

func (r *codexRun) emitResult(id, out string, isError bool) error {
	call := r.calls[id]
	meta := ""
	if !isError {
		meta = resultMetadata(call[0], call[1], out)
		if r.backgrounded[id] {
			meta = markBackground(call[0], meta)
		}
	}
	return r.t.Emit(Event{Type: EventToolResult, ID: id, Name: call[0], Output: out, Metadata: meta, IsError: isError})
}

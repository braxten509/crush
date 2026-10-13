package cliagent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/diff"
	"github.com/charmbracelet/crush/internal/filechange"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/secretguard"
	"github.com/charmbracelet/crush/internal/version"
	"mvdan.cc/sh/v3/shell"
)

// Codex is driven through `codex app-server`, its JSON-RPC protocol for rich
// clients (the one its IDE extension uses). `codex app-server generate-ts`
// prints the schema.

type codexItem struct {
	Type             string          `json:"type"`
	ID               string          `json:"id"`
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
	key := codexKey{dir: m.Dir, model: m.ID, tier: m.ServiceTier, approval: r.approval, sandbox: r.sandbox}
	// A sub-agent runs one turn, so its process isn't kept for more.
	keep := !t.NoTools && t.SessionID != "" && !m.Guarded
	var live *codexLive
	if keep {
		live = codexSessions.take(t.SessionID, key, t.Resume)
	}
	var lines <-chan []byte
	if live != nil {
		r.p, lines, r.threadID = live.p, live.lines, live.thread
	} else {
		// Crush hands every CLI the shared memory; Codex's own stays off.
		var err error
		args := []string{"app-server", "--disable", "memories", "-c", "project_doc_max_bytes=0", "-c", codexGuardHook()}
		if r.p, err = startReviewProc(m.Dir, t.Env, !t.NoTools, "codex", args...); err != nil {
			return err
		}
		lines = r.p.readLines()
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

	stop := p.watchCancel(ctx, func() {
		if ids := r.active.Load(); ids != nil {
			_ = p.send(map[string]any{"id": "4", "method": "turn/interrupt", "params": map[string]any{"threadId": ids[0], "turnId": ids[1]}})
		} else {
			p.closeInput()
		}
	})
	defer stop()

	r.stopSteer = pollSteerInputRetry(r.t, func() bool { return r.active.Load() != nil }, r.steer)
	defer r.stopSteer()

	if live != nil {
		if err := r.t.Emit(Event{Type: EventSession, Session: r.threadID}); err != nil {
			return err
		}
		r.startTurn(codexInput(withSavedImagePaths(t.Prompt, t.Attachments), t.Attachments))
	} else {
		_ = p.send(map[string]any{"id": codexInitID, "method": "initialize", "params": map[string]any{
			"clientInfo": map[string]any{"name": "crush", "title": "Crush", "version": version.Version},
			// For thread/backgroundTerminals/list after Ctrl+B.
			"capabilities": map[string]any{"experimentalApi": true},
		}})
	}

	if t.SessionID != "" && !t.NoTools {
		defer onBackground(t.SessionID, func() bool {
			if r.active.Load() == nil {
				return false
			}
			r.ctrlB.Store(true)
			if r.running.Load() > 0 {
				r.interruptForBg()
			}
			return true
		})()
	}

	for raw := range lines {
		var msg rpcMessage
		if json.Unmarshal(raw, &msg) != nil {
			continue
		}
		id := strings.Trim(string(msg.ID), `"`)

		// Server-to-client requests: approvals. They block on the user, so
		// answer them off the read loop.
		if msg.Method != "" && id != "" {
			var params codexParams
			_ = json.Unmarshal(msg.Params, &params)
			go codexRequest(ctx, m, r.t, p, msg, params, r.changes[params.ItemID])
			continue
		}
		if ctx.Err() != nil {
			// Interrupted: once the turn winds down the process is kept, as
			// exiting would stop the commands it runs in the background.
			r.stopSteer()
			if msg.Method == "turn/completed" {
				r.finished = true
				return ctx.Err()
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
			return err
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if live != nil {
		return errors.New("Codex: the kept process ended")
	}
	return exitError("codex", p)
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

// handleResponse handles Codex's response to one of Crush's requests. An
// error ends the turn.
func (r *codexRun) handleResponse(id string, msg rpcMessage) error {
	switch {
	case id == "crush-context-config":
		if msg.Error == nil {
			var res struct {
				Config codexContextSettings `json:"config"`
			}
			if json.Unmarshal(msg.Result, &res) == nil {
				r.contextSettings = res.Config
			}
		}
		return r.reportContext()
	case id == "ctrl-b-list":
		r.continueAfterCtrlB(msg)
		return nil
	case id == codexHooksID || id == codexTrustID:
		// A Codex without hooks still runs; Crush's own checks remain.
		if msg.Error != nil {
			slog.Warn("Codex did not take Crush's secrets guard", "error", msg.Error.Message)
		}
		if trust := codexGuardTrust(msg.Result); id == codexHooksID && msg.Error == nil && trust != nil {
			_ = r.p.send(trust)
		} else {
			r.startThread()
		}
		return nil
	case strings.HasPrefix(id, "steer-"):
		// A steer that missed the turn is never confirmed, so it
		// runs as the next turn instead.
		if msg.Error != nil {
			slog.Debug("Codex did not take a steered message", "error", msg.Error.Message)
		}
		return nil
	}
	if msg.Error != nil {
		if id == codexThreadID && r.t.Resume != "" {
			return ErrResume
		}
		return fmt.Errorf("Codex: %s", msg.Error.Message)
	}
	switch id {
	case codexInitID:
		_ = r.p.send(map[string]any{"method": "initialized"})
		_ = r.p.send(map[string]any{"id": codexHooksID, "method": "hooks/list", "params": map[string]any{"cwds": []string{r.m.Dir}}})
	case codexThreadID:
		var res struct {
			Thread struct {
				ID string `json:"id"`
			} `json:"thread"`
		}
		_ = json.Unmarshal(msg.Result, &res)
		r.threadID = res.Thread.ID
		if err := r.t.Emit(Event{Type: EventSession, Session: r.threadID}); err != nil {
			return err
		}
		r.startTurn(codexInput(withSavedImagePaths(r.t.Prompt, r.t.Attachments), r.t.Attachments))
	case codexTurnID:
		var res struct {
			Turn struct {
				ID string `json:"id"`
			} `json:"turn"`
		}
		_ = json.Unmarshal(msg.Result, &res)
		r.active.Store(&[2]string{r.threadID, res.Turn.ID})
	}
	return nil
}

// continueAfterCtrlB starts the turn that tells the model which terminal
// sessions its backgrounded commands run in.
func (r *codexRun) continueAfterCtrlB(msg rpcMessage) {
	var res struct {
		Data []struct {
			ItemID    string `json:"itemId"`
			ProcessID string `json:"processId"`
		} `json:"data"`
	}
	_ = json.Unmarshal(msg.Result, &res)
	var sessions []string
	var all []string
	for _, d := range res.Data {
		all = append(all, d.ProcessID)
		if r.backgrounded[d.ItemID] {
			sessions = append(sessions, d.ProcessID)
		}
	}
	if len(sessions) == 0 {
		sessions = all
	}
	r.startTurn([]any{map[string]any{"type": "text", "text": codexCtrlB(sessions), "text_elements": []any{}}})
}

// handleNotification handles a notification from Codex. done reports that
// the turn ended, with err.
func (r *codexRun) handleNotification(msg rpcMessage) (done bool, err error) {
	var params codexParams
	if (strings.HasPrefix(msg.Method, "item/") || msg.Method == "thread/tokenUsage/updated") && time.Since(r.lastActivity) >= time.Second {
		if err := r.t.Emit(Event{Type: EventActivity}); err != nil {
			return true, err
		}
		r.lastActivity = time.Now()
	}
	_ = json.Unmarshal(msg.Params, &params)
	switch msg.Method {
	case "item/agentMessage/delta":
		if r.compacting {
			break
		}
		r.textOpen = true
		err = r.t.Emit(Event{Type: EventText, Text: params.Delta})
	case "item/reasoning/summaryTextDelta", "item/reasoning/textDelta":
		if r.compacting {
			break
		}
		err = r.t.Emit(Event{Type: EventReasoning, Text: params.Delta})
	case "item/commandExecution/outputDelta":
		if r.output[params.ItemID] == nil {
			r.output[params.ItemID] = &strings.Builder{}
		}
		r.output[params.ItemID].WriteString(params.Delta)
	case "thread/tokenUsage/updated":
		err = r.handleTokenUsage(params)
	case "account/rateLimits/updated":
		var up struct {
			RateLimits codexSnapshot `json:"rateLimits"`
		}
		_ = json.Unmarshal(msg.Params, &up)
		setLimits(config.TypeCodexCLI, up.RateLimits.limits())
	case "turn/completed":
		return r.handleTurnCompleted(params)
	case "thread/compacted":
		r.compacting = false
		err = r.t.Emit(Event{Type: EventCompacting})
	case "item/started":
		err = r.handleItemStarted(params.Item)
	case "item/completed":
		err = r.handleItemCompleted(params.Item)
	}
	return err != nil, err
}

func (r *codexRun) handleTokenUsage(params codexParams) error {
	u := params.TokenUsage
	if u == nil {
		return nil
	}
	if u.ModelContextWindow > 0 {
		r.reportedWindow = u.ModelContextWindow
		if !r.t.NoTools && !r.contextRequested {
			r.contextRequested = true
			_ = r.p.send(map[string]any{"id": "crush-context-config", "method": "config/read", "params": map[string]any{"cwd": r.m.Dir, "includeLayers": false}})
		}
	}
	if err := r.reportContext(); err != nil {
		return err
	}
	return r.t.Emit(Event{Type: EventUsage, Usage: fantasy.Usage{
		InputTokens:     u.Last.InputTokens - u.Last.CachedInputTokens,
		OutputTokens:    u.Last.OutputTokens,
		ReasoningTokens: u.Last.ReasoningTokens,
		TotalTokens:     u.Last.TotalTokens,
		CacheReadTokens: u.Last.CachedInputTokens,
	}})
}

// handleTurnCompleted ends the turn, unless Ctrl+B interrupted it to move
// commands to the background: then a follow-up turn starts once their
// terminal sessions are known.
func (r *codexRun) handleTurnCompleted(params codexParams) (done bool, err error) {
	if params.Turn.Status == "interrupted" && r.movedToBg.Load() && r.ctx.Err() == nil {
		r.movedToBg.Store(false)
		r.active.Store(nil)
		for id := range r.openCmds {
			r.backgrounded[id] = true
			if err := r.emitResult(id, "Moved to the background (Ctrl+B). Still running.", false); err != nil {
				return true, err
			}
		}
		clear(r.openCmds)
		r.running.Store(0)
		// The follow-up turn starts once the sessions are known.
		_ = r.p.send(map[string]any{"id": "ctrl-b-list", "method": "thread/backgroundTerminals/list", "params": map[string]any{"threadId": r.threadID}})
		return false, nil
	}
	switch params.Turn.Status {
	case "completed":
		r.finished = true
		return true, nil
	case "interrupted":
		return true, context.Canceled
	}
	if e := params.Turn.Error; e != nil {
		return true, errors.New("Codex: " + e.Message)
	}
	return true, errors.New("Codex: turn " + params.Turn.Status)
}

func (r *codexRun) handleItemStarted(item codexItem) (err error) {
	// A command Codex handed back still running (its yield time ran
	// out) goes on as a background terminal once the model moves on.
	// Its item stays open until it exits.
	if item.Type != "userMessage" {
		for id, started := range r.openCmds {
			if time.Since(started) <= tools.ForegroundWaitLimit {
				continue
			}
			delete(r.openCmds, id)
			r.running.Add(-1)
			r.backgrounded[id] = true
			out := ""
			if b := r.output[id]; b != nil {
				out = strings.TrimRight(b.String(), "\n") + "\n\n"
			}
			if err := r.emitResult(id, strings.TrimLeft(out, "\n")+"Still running in the background.", false); err != nil {
				return err
			}
		}
	}
	switch item.Type {
	case "reasoning":
		if r.compacting {
			break
		}
		// Some models expose reasoning lifecycle without text deltas.
		// Preserve the activity state without manufacturing or displaying thoughts.
		err = r.t.Emit(Event{Type: EventReasoning})
	case "contextCompaction":
		r.compacting = true
		err = r.t.Emit(Event{Type: EventCompacting, Compacting: true})
	case "userMessage":
		var text strings.Builder
		for _, c := range item.Content {
			text.WriteString(c.Text)
		}
		if typed := withoutImagePaths(text.String()); r.sent.take(typed) {
			err = r.t.Emit(Event{Type: EventUserMessage, Text: typed})
		}
	case "agentMessage":
		if r.compacting {
			break
		}
		// Consecutive messages without tools in between would run
		// together.
		if r.textOpen {
			err = r.t.Emit(Event{Type: EventText, Text: "\n\n"})
		}
	case "commandExecution":
		r.openCmds[item.ID] = time.Now()
		r.running.Add(1)
		err = r.emitCall(item.ID, tools.BashToolName, marshal(map[string]string{"command": unwrapShell(item.Command), "description": ""}))
		if r.ctrlB.Load() {
			r.interruptForBg()
		}
	case "fileChange":
		r.changes[item.ID] = item.Changes
		for i, c := range item.Changes {
			name, input := codexChangeTool(c)
			if err = r.emitCall(item.ID+"#"+strconv.Itoa(i), name, input); err != nil {
				break
			}
		}
	case "mcpToolCall":
		err = r.emitCall(item.ID, "mcp_"+item.Server+"_"+item.Tool, string(item.Arguments))
	case "webSearch":
		err = r.emitCall(item.ID, tools.WebSearchToolName, marshal(map[string]string{"query": item.Query}))
	}
	return err
}

func (r *codexRun) handleItemCompleted(item codexItem) (err error) {
	failed := item.Status == "failed" || item.Status == "declined"
	switch item.Type {
	case "contextCompaction":
		r.compacting = false
		err = r.t.Emit(Event{Type: EventCompacting})
	case "commandExecution":
		if _, ok := r.calls[item.ID]; !ok || r.backgrounded[item.ID] {
			break // backgrounded here or in an earlier turn; the model reads its output itself
		}
		if _, ok := r.openCmds[item.ID]; ok {
			delete(r.openCmds, item.ID)
			r.running.Add(-1)
		}
		out := ""
		if item.AggregatedOutput != nil {
			out = *item.AggregatedOutput
		} else if b := r.output[item.ID]; b != nil {
			out = b.String()
		}
		if item.ExitCode != nil && *item.ExitCode != 0 {
			out = strings.TrimRight(out, "\n") + fmt.Sprintf("\n\nExit code %d", *item.ExitCode)
		}
		if item.Status == "declined" {
			out = "The user denied this command."
		}
		err = r.emitResult(item.ID, strings.TrimLeft(out, "\n"), failed)
	case "fileChange":
		out := "Applied"
		if failed {
			out = "Patch " + item.Status
		}
		for i, c := range r.changes[item.ID] {
			callID := item.ID + "#" + strconv.Itoa(i)
			if failed {
				err = r.emitResult(callID, out, true)
			} else {
				// Codex has usually written the file before Crush reads
				// its "before" copy, so rebuild it from the patch.
				err = r.t.Emit(Event{Type: EventToolResult, ID: callID, Name: r.calls[callID][0], Output: out, Metadata: codexChangeMetadata(r.m.Dir, c)})
			}
			if err != nil {
				break
			}
		}
	case "mcpToolCall":
		var parts []string
		if item.Result != nil {
			for _, c := range item.Result.Content {
				if c.Type == "text" {
					parts = append(parts, c.Text)
				}
			}
		}
		if item.Error != nil {
			failed, parts = true, []string{item.Error.Message}
		}
		err = r.emitResult(item.ID, strings.Join(parts, "\n"), failed)
	case "webSearch":
		err = r.emitResult(item.ID, "Searched the web", failed)
	}
	return err
}

// codexInput includes the captured image bytes, including clipboard images
// that have no persistent file on disk.
func codexInput(prompt string, attachments []message.Attachment) []any {
	var input []any
	if prompt != "" || !slices.ContainsFunc(attachments, message.Attachment.IsImage) {
		input = append(input, map[string]any{"type": "text", "text": prompt, "text_elements": []any{}})
	}
	for _, attachment := range attachments {
		if !attachment.IsImage() {
			continue
		}
		input = append(input, map[string]any{
			"type": "image",
			"url":  "data:" + attachment.MimeType + ";base64," + base64.StdEncoding.EncodeToString(attachment.Content),
		})
	}
	return input
}

// codexRequest answers a server request. Command and file approvals go to
// Crush's permission dialog; anything else is declined.
func codexRequest(ctx context.Context, m *Model, t Turn, p *proc, msg rpcMessage, params codexParams, changes []codexChange) {
	decide := func(ok bool) {
		decision := "decline"
		if ok {
			decision = "accept"
		}
		_ = p.send(map[string]any{"id": msg.ID, "result": map[string]any{"decision": decision}})
	}
	switch msg.Method {
	case "item/commandExecution/requestApproval":
		input := marshal(map[string]string{"command": unwrapShell(params.Command), "description": ""})
		decide(!t.NoTools && m.approve(ctx, t.SessionID, params.ItemID, tools.BashToolName, input))
	case "item/fileChange/requestApproval":
		ok := !t.NoTools
		for i, c := range changes {
			name, input := codexChangeTool(c)
			ok = ok && m.approve(ctx, t.SessionID, params.ItemID+"#"+strconv.Itoa(i), name, input)
		}
		decide(ok)
	default:
		_ = p.send(map[string]any{"id": msg.ID, "error": map[string]any{"code": -32601, "message": "not supported by Crush"}})
	}
}

// codexChangeTool presents one file of a Codex patch as a Crush write (new
// file) or edit (changed or deleted file).
func codexChangeTool(c codexChange) (string, string) {
	if c.Kind.Type == "add" {
		return tools.WriteToolName, marshal(tools.WriteParams{FilePath: c.Path, Content: c.Diff})
	}
	oldText, newText := splitDiff(c.Diff)
	if c.Kind.Type == "delete" {
		return tools.EditToolName, marshal(struct {
			tools.EditParams
			DeleteFile bool `json:"delete_file"`
		}{tools.EditParams{FilePath: c.Path, OldString: oldText, NewString: newText}, true})
	}
	return tools.EditToolName, marshal(tools.EditParams{FilePath: c.Path, OldString: oldText, NewString: newText})
}

// codexChangeMetadata describes a finished change with the whole file before
// and after it. The after text is read from disk; the before text comes from
// undoing the reported patch, so it does not depend on when Crush looked at
// the file. It returns "" when the patch no longer matches the file.
func codexChangeMetadata(dir string, c codexChange) string {
	if c.Kind.Type == "delete" {
		return filechange.WithReview("", &filechange.Review{Root: dir, Deletions: true})
	}
	if !filechange.InRepository(filechange.RepositoryRoot(dir), dir, c.Path) {
		return filechange.WithReview("", &filechange.Review{Root: dir})
	}
	path := c.Path
	if !filepath.IsAbs(path) {
		path = filepath.Join(dir, path)
	}
	var before, after string
	switch c.Kind.Type {
	case "add":
		data, err := os.ReadFile(path)
		if err != nil {
			return ""
		}
		after = string(data)
	default:
		data, err := os.ReadFile(path)
		if err != nil {
			return ""
		}
		after = string(data)
		var ok bool
		if before, ok = reversePatch(after, c.Diff); !ok {
			return ""
		}
	}
	_, adds, dels := diff.GenerateDiff(before, after, path)
	metadata := marshal(tools.EditResponseMetadata{Additions: adds, Removals: dels, OldContent: before, NewContent: after})
	mode := uint32(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = uint32(info.Mode())
	}
	change := filechange.Change{Path: path, Order: time.Now().UnixNano()}
	if c.Kind.Type != "add" {
		change.Before = &filechange.State{Content: before, Size: int64(len(before)), Mode: mode}
	}
	change.After = &filechange.State{Content: after, Size: int64(len(after)), Mode: mode}
	return filechange.WithReview(metadata, &filechange.Review{Root: dir, Changes: []filechange.Change{change}})
}

// reversePatch undoes a unified diff's hunks on the patched text, in order.
func reversePatch(patched, patch string) (string, bool) {
	type hunk struct {
		oldLines, newLines     []string
		at, oldCount, newCount int
	}
	header := regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)
	var hunks []hunk
	var current *hunk
	previous := byte(0)
	for _, line := range strings.Split(strings.TrimSuffix(patch, "\n"), "\n") {
		if strings.HasPrefix(line, "@@") {
			match := header.FindStringSubmatch(line)
			if match == nil {
				return "", false
			}
			at, _ := strconv.Atoi(match[3])
			oldCount, newCount := 1, 1
			if match[2] != "" {
				oldCount, _ = strconv.Atoi(match[2])
			}
			if match[4] != "" {
				newCount, _ = strconv.Atoi(match[4])
			}
			if newCount > 0 {
				at--
			}
			hunks = append(hunks, hunk{at: at, oldCount: oldCount, newCount: newCount})
			current = &hunks[len(hunks)-1]
			previous = 0
			continue
		}
		if current == nil {
			continue
		}
		if strings.HasPrefix(line, `\ No newline at end of file`) {
			if (previous == '-' || previous == ' ') && len(current.oldLines) > 0 {
				n := len(current.oldLines) - 1
				current.oldLines[n] = strings.TrimSuffix(current.oldLines[n], "\n")
			}
			if (previous == '+' || previous == ' ') && len(current.newLines) > 0 {
				n := len(current.newLines) - 1
				current.newLines[n] = strings.TrimSuffix(current.newLines[n], "\n")
			}
			continue
		}
		if line == "" {
			return "", false
		}
		previous = line[0]
		switch previous {
		case '-':
			current.oldLines = append(current.oldLines, line[1:]+"\n")
		case '+':
			current.newLines = append(current.newLines, line[1:]+"\n")
		case ' ':
			current.oldLines = append(current.oldLines, line[1:]+"\n")
			current.newLines = append(current.newLines, line[1:]+"\n")
		default:
			return "", false
		}
	}
	if len(hunks) == 0 {
		return "", false
	}
	lines := strings.SplitAfter(patched, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	var output strings.Builder
	position := 0
	for _, h := range hunks {
		if len(h.oldLines) != h.oldCount || len(h.newLines) != h.newCount || h.at < position || h.at+len(h.newLines) > len(lines) {
			return "", false
		}
		if !slices.Equal(lines[h.at:h.at+len(h.newLines)], h.newLines) {
			return "", false
		}
		output.WriteString(strings.Join(lines[position:h.at], ""))
		output.WriteString(strings.Join(h.oldLines, ""))
		position = h.at + len(h.newLines)
	}
	output.WriteString(strings.Join(lines[position:], ""))
	return output.String(), true
}

// splitDiff rebuilds the before and after text of a unified diff's hunks,
// which is what Crush's edit view diffs.
func splitDiff(diff string) (string, string) {
	var before, after strings.Builder
	for i, line := range strings.Split(strings.TrimRight(diff, "\n"), "\n") {
		switch {
		case strings.HasPrefix(line, "@@"):
			if i > 0 {
				before.WriteString("…\n")
				after.WriteString("…\n")
			}
		case strings.HasPrefix(line, "-"):
			before.WriteString(line[1:] + "\n")
		case strings.HasPrefix(line, "+"):
			after.WriteString(line[1:] + "\n")
		case strings.HasPrefix(line, `\`): // "\ No newline at end of file"
		default:
			line = strings.TrimPrefix(line, " ")
			before.WriteString(line + "\n")
			after.WriteString(line + "\n")
		}
	}
	return before.String(), after.String()
}

var shellWrapper = regexp.MustCompile(`(?s)^\S*sh -l?c (?:'(.*)'|"(.*)")$`)

// unwrapShell turns Codex's `/bin/zsh -lc 'cmd'` into `cmd` for display.
func unwrapShell(cmd string) string {
	// Providers may wrap an already wrapped command. Decode each shell argv
	// rather than stripping its first and last quote: mixed shell quoting can
	// start with a single quote and end with a double quote.
	for {
		decoded := unwrapShellOnce(cmd)
		if decoded == cmd {
			return cmd
		}
		cmd = decoded
	}
}

func unwrapShellOnce(cmd string) string {
	m := shellWrapper.FindStringSubmatch(cmd)
	if m != nil && m[2] != "" {
		// Preserve Codex's JSON-style command representation, including newlines.
		if command, err := strconv.Unquote(`"` + m[2] + `"`); err == nil {
			return command
		}
	}
	words, err := shell.Fields(cmd, func(name string) string { return "$" + name })
	if err == nil && len(words) == 3 && strings.HasSuffix(filepath.Base(words[0]), "sh") &&
		(words[1] == "-lc" || words[1] == "-c") {
		return words[2]
	}
	return cmd
}

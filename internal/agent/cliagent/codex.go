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
	"sync"
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
type rpcMessage struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error"`
}

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
	p       *proc
	lines   <-chan []byte
	key     codexKey
	thread  string
	idle    chan struct{} // closed to end the between-turns drain
	drained chan struct{}
	dead    atomic.Bool
	timer   *time.Timer
}

var codexSessions = struct {
	mu sync.Mutex
	m  map[string]*codexLive
}{m: map[string]*codexLive{}}

// takeCodex hands over the session's live process if it can run this turn,
// closing it otherwise.
// ponytail: a turn with other settings closes it, stopping any commands it
// moved to the background; so does [claudeIdle] without a turn.
func takeCodex(sessionID string, key codexKey, resume string) *codexLive {
	codexSessions.mu.Lock()
	l := codexSessions.m[sessionID]
	delete(codexSessions.m, sessionID)
	codexSessions.mu.Unlock()
	if l == nil {
		return nil
	}
	l.timer.Stop()
	close(l.idle)
	<-l.drained
	if l.dead.Load() || l.key != key || resume == "" || resume != l.thread {
		l.p.finish()
		return nil
	}
	return l
}

// keepCodex parks a process after a finished turn until the session's next
// prompt, or closes it after [claudeIdle].
func keepCodex(sessionID string, l *codexLive) {
	l.idle, l.drained = make(chan struct{}), make(chan struct{})
	go func() {
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
		codexSessions.mu.Lock()
		if codexSessions.m[sessionID] != l {
			codexSessions.mu.Unlock()
			return
		}
		delete(codexSessions.m, sessionID)
		codexSessions.mu.Unlock()
		close(l.idle)
		<-l.drained
		l.p.finish()
	})
	codexSessions.mu.Lock()
	old := codexSessions.m[sessionID]
	codexSessions.m[sessionID] = l
	codexSessions.mu.Unlock()
	if old != nil {
		old.timer.Stop()
		close(old.idle)
		<-old.drained
		old.p.finish()
	}
}

// codexCtrlB is what Codex is told after Ctrl+B interrupted its turn; the
// interrupt kept it from learning the commands' terminal sessions.
func codexCtrlB(sessions []string) string {
	where := "in its terminal session"
	if len(sessions) > 0 {
		where = "in terminal session " + strings.Join(sessions, ", ") + " (poll it with write_stdin and empty input)"
	}
	return "The user pressed Ctrl+B to move the command you were waiting on to the background. It is still running " + where + ", so don't start it again and don't wait for it now: carry on with anything else, or say in a sentence that it's running in the background and end your turn. Check on it later, e.g. when the user asks."
}

func runCodex(ctx context.Context, m *Model, t Turn) error {
	// Crush's permission prompts replace Codex's own; the sandbox still
	// applies. YOLO mode lifts both, matching Crush's own tools.
	approval, sandbox := "on-request", "workspace-write"
	if t.NoTools || m.ReadOnly {
		approval, sandbox = "never", "read-only"
	} else if m.autoApproved(t.SessionID) {
		approval, sandbox = "never", "danger-full-access"
	}
	key := codexKey{dir: m.Dir, model: m.ID, tier: m.ServiceTier, approval: approval, sandbox: sandbox}
	// A sub-agent runs one turn, so its process isn't kept for more.
	keep := !t.NoTools && t.SessionID != "" && !m.Guarded
	var live *codexLive
	if keep {
		live = takeCodex(t.SessionID, key, t.Resume)
	}
	var (
		p        *proc
		lines    <-chan []byte
		threadID string
	)
	if live != nil {
		p, lines, threadID = live.p, live.lines, live.thread
	} else {
		// Crush hands every CLI the shared memory; Codex's own stays off.
		var err error
		args := []string{"app-server", "--disable", "memories", "-c", "project_doc_max_bytes=0", "-c", codexGuardHook()}
		if p, err = startReviewProc(m.Dir, t.Env, !t.NoTools, "codex", args...); err != nil {
			return err
		}
		lines = p.readLines()
	}
	t.Emit = p.reviewEvents(t.Emit)
	var contextSettings codexContextSettings
	catalogWindow, catalogLimit, effectivePercent := codexCatalogContext(m.ID)
	var reportedWindow int64
	contextRequested := false
	reportContext := func() error {
		if t.NoTools {
			return nil
		}
		event := codexContextBudget(contextSettings, catalogWindow, catalogLimit, reportedWindow, effectivePercent)
		if event.ContextLimit <= 0 {
			return nil
		}
		return t.Emit(event)
	}
	compacting := false
	lastActivity := time.Time{}
	finished := false
	defer func() {
		if finished && keep && threadID != "" {
			keepCodex(t.SessionID, &codexLive{p: p, lines: lines, key: key, thread: threadID})
		} else {
			p.finish()
		}
	}()

	// Thread and turn IDs, read by the cancel watcher.
	var active atomic.Pointer[[2]string]
	stop := p.watchCancel(ctx, func() {
		if ids := active.Load(); ids != nil {
			_ = p.send(map[string]any{"id": "4", "method": "turn/interrupt", "params": map[string]any{"threadId": ids[0], "turnId": ids[1]}})
		} else {
			p.closeInput()
		}
	})
	defer stop()

	// Messages queued mid-turn go in through turn/steer; Codex reports each
	// as a userMessage item when the model takes it in.
	var sent steered
	steers := 0
	stopSteer := pollSteerInputRetry(t, func() bool { return active.Load() != nil }, func(text string, attachments []message.Attachment) bool {
		ids := active.Load()
		if ids == nil {
			return false
		}
		steers++
		sent.add(text)
		err := p.send(map[string]any{"id": "steer-" + strconv.Itoa(steers), "method": "turn/steer", "params": map[string]any{
			"threadId":       ids[0],
			"expectedTurnId": ids[1],
			"input":          codexInput(withSavedImagePaths(text, attachments), attachments),
		}})
		if err != nil {
			sent.take(text)
		}
		return true
	})
	defer stopSteer()

	startTurn := func(input []any) {
		params := map[string]any{"threadId": threadID, "input": input}
		if t.Effort != "" {
			params["effort"] = t.Effort
		}
		if m.ServiceTier != "" {
			params["serviceTier"] = m.ServiceTier
		}
		_ = p.send(map[string]any{"id": codexTurnID, "method": "turn/start", "params": params})
	}
	startThread := func() {
		params := map[string]any{"cwd": m.Dir, "model": m.ID, "approvalPolicy": approval, "sandbox": sandbox}
		if m.ServiceTier != "" {
			params["serviceTier"] = m.ServiceTier
		}
		method := "thread/start"
		if t.Resume != "" {
			method, params["threadId"] = "thread/resume", t.Resume
		} else if t.NoTools {
			params["ephemeral"] = true
		}
		_ = p.send(map[string]any{"id": codexThreadID, "method": method, "params": params})
	}
	if live != nil {
		if err := t.Emit(Event{Type: EventSession, Session: threadID}); err != nil {
			return err
		}
		startTurn(codexInput(withSavedImagePaths(t.Prompt, t.Attachments), t.Attachments))
	} else {
		_ = p.send(map[string]any{"id": codexInitID, "method": "initialize", "params": map[string]any{
			"clientInfo": map[string]any{"name": "crush", "title": "Crush", "version": version.Version},
			// For thread/backgroundTerminals/list after Ctrl+B.
			"capabilities": map[string]any{"experimentalApi": true},
		}})
	}

	// Ctrl+B interrupts the turn: Codex keeps its commands running in
	// their terminal sessions, and a new turn tells the model so.
	var running atomic.Int32 // commands in progress
	var ctrlB, movedToBg atomic.Bool
	interruptForBg := func() {
		if ids := active.Load(); ids != nil && movedToBg.CompareAndSwap(false, true) {
			ctrlB.Store(false)
			_ = p.send(map[string]any{"id": "ctrl-b", "method": "turn/interrupt", "params": map[string]any{"threadId": ids[0], "turnId": ids[1]}})
		}
	}
	if t.SessionID != "" && !t.NoTools {
		defer onBackground(t.SessionID, func() bool {
			if active.Load() == nil {
				return false
			}
			ctrlB.Store(true)
			if running.Load() > 0 {
				interruptForBg()
			}
			return true
		})()
	}
	openCmds := map[string]time.Time{}
	backgrounded := map[string]bool{} // their results are already shown

	// Crush tool name and input per item (file changes expand to one call
	// per file), and command output streamed so far.
	calls := map[string][2]string{}
	changes := map[string][]codexChange{}
	output := map[string]*strings.Builder{}
	textOpen := false

	emitCall := func(id, name, input string) error {
		calls[id] = [2]string{name, input}
		textOpen = false
		if err := t.Emit(Event{Type: EventToolStart, ID: id, Name: name}); err != nil {
			return err
		}
		return t.Emit(Event{Type: EventToolCall, ID: id, Name: name, Input: input})
	}
	emitResult := func(id, out string, isError bool) error {
		call := calls[id]
		meta := ""
		if !isError {
			meta = resultMetadata(call[0], call[1], out)
			if backgrounded[id] {
				meta = markBackground(call[0], meta)
			}
		}
		return t.Emit(Event{Type: EventToolResult, ID: id, Name: call[0], Output: out, Metadata: meta, IsError: isError})
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
			go codexRequest(ctx, m, t, p, msg, params, changes[params.ItemID])
			continue
		}
		if ctx.Err() != nil {
			// Interrupted: once the turn winds down the process is kept, as
			// exiting would stop the commands it runs in the background.
			stopSteer()
			if msg.Method == "turn/completed" {
				finished = true
				return ctx.Err()
			}
			continue
		}

		// Responses to our requests.
		if msg.Method == "" && id == "crush-context-config" {
			if msg.Error == nil {
				var res struct {
					Config codexContextSettings `json:"config"`
				}
				if json.Unmarshal(msg.Result, &res) == nil {
					contextSettings = res.Config
				}
			}
			if err := reportContext(); err != nil {
				return err
			}
			continue
		}
		if msg.Method == "" && id == "ctrl-b-list" {
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
				if backgrounded[d.ItemID] {
					sessions = append(sessions, d.ProcessID)
				}
			}
			if len(sessions) == 0 {
				sessions = all
			}
			startTurn([]any{map[string]any{"type": "text", "text": codexCtrlB(sessions), "text_elements": []any{}}})
			continue
		}
		if msg.Method == "" && (id == codexHooksID || id == codexTrustID) {
			// A Codex without hooks still runs; Crush's own checks remain.
			if msg.Error != nil {
				slog.Warn("Codex did not take Crush's secrets guard", "error", msg.Error.Message)
			}
			if trust := codexGuardTrust(msg.Result); id == codexHooksID && msg.Error == nil && trust != nil {
				_ = p.send(trust)
			} else {
				startThread()
			}
			continue
		}
		if msg.Method == "" {
			if strings.HasPrefix(id, "steer-") {
				// A steer that missed the turn is never confirmed, so it
				// runs as the next turn instead.
				if msg.Error != nil {
					slog.Debug("Codex did not take a steered message", "error", msg.Error.Message)
				}
				continue
			}
			if msg.Error != nil {
				if id == codexThreadID && t.Resume != "" {
					return ErrResume
				}
				return fmt.Errorf("Codex: %s", msg.Error.Message)
			}
			switch id {
			case codexInitID:
				_ = p.send(map[string]any{"method": "initialized"})
				_ = p.send(map[string]any{"id": codexHooksID, "method": "hooks/list", "params": map[string]any{"cwds": []string{m.Dir}}})
			case codexThreadID:
				var res struct {
					Thread struct {
						ID string `json:"id"`
					} `json:"thread"`
				}
				_ = json.Unmarshal(msg.Result, &res)
				threadID = res.Thread.ID
				if err := t.Emit(Event{Type: EventSession, Session: threadID}); err != nil {
					return err
				}
				startTurn(codexInput(withSavedImagePaths(t.Prompt, t.Attachments), t.Attachments))
			case codexTurnID:
				var res struct {
					Turn struct {
						ID string `json:"id"`
					} `json:"turn"`
				}
				_ = json.Unmarshal(msg.Result, &res)
				active.Store(&[2]string{threadID, res.Turn.ID})
			}
			continue
		}

		var params codexParams
		if (strings.HasPrefix(msg.Method, "item/") || msg.Method == "thread/tokenUsage/updated") && time.Since(lastActivity) >= time.Second {
			if err := t.Emit(Event{Type: EventActivity}); err != nil {
				return err
			}
			lastActivity = time.Now()
		}
		_ = json.Unmarshal(msg.Params, &params)
		item := params.Item
		var err error
		switch msg.Method {
		case "item/agentMessage/delta":
			if compacting {
				break
			}
			textOpen = true
			err = t.Emit(Event{Type: EventText, Text: params.Delta})
		case "item/reasoning/summaryTextDelta", "item/reasoning/textDelta":
			if compacting {
				break
			}
			err = t.Emit(Event{Type: EventReasoning, Text: params.Delta})
		case "item/commandExecution/outputDelta":
			if output[params.ItemID] == nil {
				output[params.ItemID] = &strings.Builder{}
			}
			output[params.ItemID].WriteString(params.Delta)
		case "thread/tokenUsage/updated":
			if u := params.TokenUsage; u != nil {
				if u.ModelContextWindow > 0 {
					reportedWindow = u.ModelContextWindow
					if !t.NoTools && !contextRequested {
						contextRequested = true
						_ = p.send(map[string]any{"id": "crush-context-config", "method": "config/read", "params": map[string]any{"cwd": m.Dir, "includeLayers": false}})
					}
				}
				if err := reportContext(); err != nil {
					return err
				}
				err = t.Emit(Event{Type: EventUsage, Usage: fantasy.Usage{
					InputTokens:     u.Last.InputTokens - u.Last.CachedInputTokens,
					OutputTokens:    u.Last.OutputTokens,
					ReasoningTokens: u.Last.ReasoningTokens,
					TotalTokens:     u.Last.TotalTokens,
					CacheReadTokens: u.Last.CachedInputTokens,
				}})
			}
		case "account/rateLimits/updated":
			var up struct {
				RateLimits codexSnapshot `json:"rateLimits"`
			}
			_ = json.Unmarshal(msg.Params, &up)
			setLimits(config.TypeCodexCLI, up.RateLimits.limits())
		case "turn/completed":
			if params.Turn.Status == "interrupted" && movedToBg.Load() && ctx.Err() == nil {
				movedToBg.Store(false)
				active.Store(nil)
				for id := range openCmds {
					backgrounded[id] = true
					if err := emitResult(id, "Moved to the background (Ctrl+B). Still running.", false); err != nil {
						return err
					}
				}
				clear(openCmds)
				running.Store(0)
				// The follow-up turn starts once the sessions are known.
				_ = p.send(map[string]any{"id": "ctrl-b-list", "method": "thread/backgroundTerminals/list", "params": map[string]any{"threadId": threadID}})
				continue
			}
			switch params.Turn.Status {
			case "completed":
				finished = true
				return nil
			case "interrupted":
				return context.Canceled
			}
			if e := params.Turn.Error; e != nil {
				return errors.New("Codex: " + e.Message)
			}
			return errors.New("Codex: turn " + params.Turn.Status)

		case "thread/compacted":
			compacting = false
			err = t.Emit(Event{Type: EventCompacting})

		case "item/started":
			// A command Codex handed back still running (its yield time ran
			// out) goes on as a background terminal once the model moves on.
			// Its item stays open until it exits.
			if item.Type != "userMessage" {
				for id, started := range openCmds {
					if time.Since(started) <= tools.ForegroundWaitLimit {
						continue
					}
					delete(openCmds, id)
					running.Add(-1)
					backgrounded[id] = true
					out := ""
					if b := output[id]; b != nil {
						out = strings.TrimRight(b.String(), "\n") + "\n\n"
					}
					if err := emitResult(id, strings.TrimLeft(out, "\n")+"Still running in the background.", false); err != nil {
						return err
					}
				}
			}
			switch item.Type {
			case "reasoning":
				if compacting {
					break
				}
				// Some models expose reasoning lifecycle without text deltas.
				// Preserve the activity state without manufacturing or displaying thoughts.
				err = t.Emit(Event{Type: EventReasoning})
			case "contextCompaction":
				compacting = true
				err = t.Emit(Event{Type: EventCompacting, Compacting: true})
			case "userMessage":
				var text strings.Builder
				for _, c := range item.Content {
					text.WriteString(c.Text)
				}
				if typed := withoutImagePaths(text.String()); sent.take(typed) {
					err = t.Emit(Event{Type: EventUserMessage, Text: typed})
				}
			case "agentMessage":
				if compacting {
					break
				}
				// Consecutive messages without tools in between would run
				// together.
				if textOpen {
					err = t.Emit(Event{Type: EventText, Text: "\n\n"})
				}
			case "commandExecution":
				openCmds[item.ID] = time.Now()
				running.Add(1)
				err = emitCall(item.ID, tools.BashToolName, marshal(map[string]string{"command": unwrapShell(item.Command), "description": ""}))
				if ctrlB.Load() {
					interruptForBg()
				}
			case "fileChange":
				changes[item.ID] = item.Changes
				for i, c := range item.Changes {
					name, input := codexChangeTool(c)
					if err = emitCall(item.ID+"#"+strconv.Itoa(i), name, input); err != nil {
						break
					}
				}
			case "mcpToolCall":
				err = emitCall(item.ID, "mcp_"+item.Server+"_"+item.Tool, string(item.Arguments))
			case "webSearch":
				err = emitCall(item.ID, tools.WebSearchToolName, marshal(map[string]string{"query": item.Query}))
			}

		case "item/completed":
			failed := item.Status == "failed" || item.Status == "declined"
			switch item.Type {
			case "contextCompaction":
				compacting = false
				err = t.Emit(Event{Type: EventCompacting})
			case "commandExecution":
				if _, ok := calls[item.ID]; !ok || backgrounded[item.ID] {
					break // backgrounded here or in an earlier turn; the model reads its output itself
				}
				if _, ok := openCmds[item.ID]; ok {
					delete(openCmds, item.ID)
					running.Add(-1)
				}
				out := ""
				if item.AggregatedOutput != nil {
					out = *item.AggregatedOutput
				} else if b := output[item.ID]; b != nil {
					out = b.String()
				}
				if item.ExitCode != nil && *item.ExitCode != 0 {
					out = strings.TrimRight(out, "\n") + fmt.Sprintf("\n\nExit code %d", *item.ExitCode)
				}
				if item.Status == "declined" {
					out = "The user denied this command."
				}
				err = emitResult(item.ID, strings.TrimLeft(out, "\n"), failed)
			case "fileChange":
				out := "Applied"
				if failed {
					out = "Patch " + item.Status
				}
				for i, c := range changes[item.ID] {
					callID := item.ID + "#" + strconv.Itoa(i)
					if failed {
						err = emitResult(callID, out, true)
					} else {
						// Codex has usually written the file before Crush reads
						// its "before" copy, so rebuild it from the patch.
						err = t.Emit(Event{Type: EventToolResult, ID: callID, Name: calls[callID][0], Output: out, Metadata: codexChangeMetadata(m.Dir, c)})
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
				err = emitResult(item.ID, strings.Join(parts, "\n"), failed)
			case "webSearch":
				err = emitResult(item.ID, "Searched the web", failed)
			}
		}
		if err != nil {
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
	return tools.EditToolName, marshal(tools.EditParams{FilePath: c.Path, OldString: oldText, NewString: newText})
}

// codexChangeMetadata describes a finished change with the whole file before
// and after it. The after text is read from disk; the before text comes from
// undoing the reported patch, so it does not depend on when Crush looked at
// the file. It returns "" when the patch no longer matches the file.
func codexChangeMetadata(dir string, c codexChange) string {
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
	case "delete":
		before = c.Diff
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
	mode := uint32(0644)
	if info, err := os.Stat(path); err == nil {
		mode = uint32(info.Mode())
	}
	change := filechange.Change{Path: path, Order: time.Now().UnixNano()}
	if c.Kind.Type != "add" {
		change.Before = &filechange.State{Content: before, Size: int64(len(before)), Mode: mode}
		if c.Kind.Type == "delete" {
			// The late deletion report has text but no original permissions. A
			// timely tracker snapshot takes precedence; otherwise never invent a
			// 0644 restore for a file that may have been executable or private.
			change.Before.RestoreOmitted = "Original permissions were not recorded for this deletion"
		}
	}
	if c.Kind.Type != "delete" {
		change.After = &filechange.State{Content: after, Size: int64(len(after)), Mode: mode}
	}
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

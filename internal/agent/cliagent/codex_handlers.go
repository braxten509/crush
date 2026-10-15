package cliagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"charm.land/fantasy"

	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/agent/tools/shelltool"
	"github.com/charmbracelet/crush/internal/config"
)

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
	case "turn/plan/updated":
		var up struct {
			Plan any `json:"plan"`
		}
		_ = json.Unmarshal(msg.Params, &up)
		if input, ok := todoInput(up.Plan); ok {
			err = r.t.Emit(Event{Type: EventTodos, Input: input})
		}
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
			if time.Since(started) <= shelltool.ForegroundWaitLimit {
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
	case "reasoning":
		if !r.compacting {
			r.cue = cueQuiet
		}
	case "agentMessage":
		if !r.compacting && item.Phase == "commentary" {
			r.cue = cueNow
		}
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

package cliagent

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/message"
)

func (r *claudeRun) interrupt() {
	_ = r.p.send(map[string]any{"type": "control_request", "request_id": "crush-interrupt", "request": map[string]any{"subtype": "interrupt"}})
}

// startSteer forwards prompts queued mid-turn to Claude until the returned
// func stops it.
func (r *claudeRun) startSteer() func() {
	return pollSteerInput(r.t, func() bool { return true }, func(text string, attachments []message.Attachment) {
		r.sent.add(text)
		prompt := claudePrompt(withSavedImagePaths(text, attachments), attachments)
		// Explicitly fold this input in at the next tool boundary. Claude's
		// default priority can be "later", which waits for the whole turn.
		prompt["priority"] = "next"
		if r.p.send(prompt) != nil {
			r.sent.take(text)
		}
	})
}

// flushThinking emits the held thinking, as narration or as reasoning.
func (r *claudeRun) flushThinking(narration bool) error {
	text := r.thinking.String()
	r.thinking.Reset()
	if text == "" {
		return nil
	}
	kind := EventReasoning
	if narration {
		kind = EventText
		if err := r.t.Emit(Event{Type: EventText, Text: TextBreak}); err != nil {
			return err
		}
	}
	return r.t.Emit(Event{Type: kind, Text: text})
}

func (r *claudeRun) requestContext() {
	if r.t.NoTools || !r.contextSupported || time.Since(r.lastContextRequest) < 30*time.Second {
		return
	}
	r.lastContextRequest = time.Now()
	_ = r.p.send(map[string]any{
		"type": "control_request", "request_id": "crush-context",
		"request": map[string]any{"subtype": "get_context_usage", "detail": "summary"},
	})
}

// handleInterrupted winds an interrupted turn down. Claude runs messages
// steered in but not yet taken right after, so that turn is stopped too.
// The process is then kept: exiting would stop its background tasks.
func (r *claudeRun) handleInterrupted(line claudeLine) (done bool, err error) {
	r.stopSteer()
	switch {
	case line.Type == "user" && line.IsReplay:
		r.sent.take(withoutImagePaths(claudeUserText(line.Message)))
	case line.Type == "system" && line.Subtype == "init" && r.interrupted > 0:
		r.interrupt()
	case line.Type == "result":
		r.interrupted++
		if r.sent.pending() == 0 || r.interrupted > 1 {
			r.finished = true
			return true, r.ctx.Err()
		}
	}
	return false, nil
}

// handleLine handles one line of Claude's output. done reports that the
// turn ended, with err.
func (r *claudeRun) handleLine(line claudeLine, raw []byte) (done bool, err error) {
	switch line.Type {
	case "control_response":
		if event, ok := claudeContextBudget(line.Response); ok {
			err = r.t.Emit(event)
		}
	case "system":
		err = r.handleSystem(line)
	case "rate_limit_event":
		setLimits(config.TypeClaudeCode, claudeRateLimit(raw))
	case "stream_event":
		err = r.handleStreamEvent(line)
	case "assistant":
		err = r.handleAssistant(line)
	case "user":
		err = r.handleUser(line)
	case "control_request":
		go claudeControl(r.ctx, r.m, r.t, r.p, line)
	case "result":
		return r.handleResult(line)
	}
	return err != nil, err
}

func (r *claudeRun) handleSystem(line claudeLine) error {
	if line.Subtype == "status" || line.Subtype == "compact_boundary" {
		if err := r.t.Emit(Event{Type: EventCompacting, Compacting: line.Status == "compacting"}); err != nil {
			return err
		}
	}
	// Only a started task can be backgrounded; asking earlier
	// finds nothing.
	if line.Subtype == "task_started" && line.ToolUseID != "" && !line.IsBackgrounded {
		if r.ctl.ctrlB.Swap(false) {
			_ = r.p.send(map[string]any{"type": "control_request", "request_id": "crush-ctrl-b-" + line.ToolUseID, "request": map[string]any{"subtype": "background_tasks", "tool_use_id": line.ToolUseID}})
		}
	}
	// Claude reports this again for each prompt a live process takes.
	if line.Subtype == "init" && line.SessionID != "" {
		r.contextSupported = line.ClaudeCodeVersion != ""
		r.requestContext()
		r.started = true
		r.live.native = line.SessionID
		if err := r.t.Emit(Event{Type: EventSession, Session: line.SessionID}); err != nil {
			return err
		}
	}
	return nil
}

func (r *claudeRun) handleStreamEvent(line claudeLine) error {
	if ev := line.Event; ev != nil && (ev.Type == "message_start" || ev.Type == "content_block_start" || ev.Type == "content_block_delta") {
		r.steerWait = nil
	}
	if err := r.recordUsage(line); err != nil {
		return err
	}
	if ev := line.Event; ev != nil && ev.Type == "content_block_start" && (ev.ContentBlock.Type == "thinking" || ev.ContentBlock.Type == "redacted_thinking") {
		return r.t.Emit(Event{Type: EventActivity, Activity: ActivityThinking})
	}
	if ev := line.Event; ev != nil && ev.Type == "content_block_delta" && ev.Delta.Type == "thinking_delta" {
		r.thinking.WriteString(ev.Delta.Thinking)
		// The held thinking isn't on screen until it ends, so pass its
		// newest line along. Summaries come in bursts with long pauses
		// between, so each delta is passed on, as text deltas are.
		if ev.Delta.Thinking == "" {
			return nil
		}
		return r.t.Emit(Event{Type: EventActivity, Activity: ActivityThinking, Text: lastLine(r.thinking.String())})
	}
	return claudeStreamEvent(line.Event, r.t.Emit)
}

func (r *claudeRun) handleAssistant(line claudeLine) error {
	r.requestContext()
	r.steerWait = nil
	if err := r.recordUsage(line); err != nil {
		return err
	}
	for index, b := range claudeBlocks(line.Message) {
		if b.Type == "thinking" {
			r.thinking.Reset()
			r.thinking.WriteString(b.Thinking)
			if err := r.flushThinking(slices.Contains(line.NarrationBlockIndexes, index)); err != nil {
				return err
			}
			continue
		}
		if b.Type != "tool_use" {
			continue
		}
		name, input := claudeTool(b.Name, b.Input)
		r.calls[b.ID] = [2]string{name, input}
		if err := r.t.Emit(Event{Type: EventToolCall, ID: b.ID, Name: name, Input: input}); err != nil {
			return err
		}
	}
	return r.emitMarker(line, MarkerForAssistant)
}

// emitMarker reports Claude's own ID for line, once the line's events are
// handled, so it lands on the message they went to.
func (r *claudeRun) emitMarker(line claudeLine, target string) error {
	if line.UUID == "" || !r.t.marks() {
		return nil
	}
	native := line.SessionID
	if native == "" && r.live != nil {
		native = r.live.native
	}
	return r.t.Emit(Event{Type: EventMarker, Marker: &Marker{Native: native, ID: line.UUID, Kind: MarkerMessage, For: target}})
}

func (r *claudeRun) handleUser(line claudeLine) error {
	if line.IsReplay {
		if text := withoutImagePaths(claudeUserText(line.Message)); r.sent.take(text) {
			r.steerWait = nil
			if err := r.t.Emit(Event{Type: EventUserMessage, Text: text}); err != nil {
				return err
			}
		} else {
			r.promptTaken = true
		}
		return nil
	}
	results := 0
	for _, b := range claudeBlocks(line.Message) {
		if b.Type != "tool_result" {
			continue
		}
		results++
		call := r.calls[b.ToolUseID]
		r.ctl.ctrlB.Store(false) // the command it was for is done
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
		if err := r.t.Emit(Event{Type: EventToolResult, ID: b.ToolUseID, Name: call[0], Output: out, Metadata: meta, IsError: b.IsError}); err != nil {
			return err
		}
	}
	if results == 0 {
		return nil
	}
	return r.emitMarker(line, MarkerForTool)
}

// handleResult ends the turn on its result, unless a message steered in
// as the model finished still has to run.
func (r *claudeRun) handleResult(line claudeLine) (done bool, err error) {
	if err := r.flushThinking(false); err != nil {
		return true, err
	}
	// A background task finishing reports its own result outside
	// the prompt; it does not end this turn.
	if r.ctx.Err() != nil {
		return true, r.ctx.Err()
	}
	notice := line.Origin != nil && line.Origin.Kind == "task-notification"
	if !r.promptTaken && notice {
		return false, nil // a reply Claude started on its own before this prompt
	}
	r.promptTaken = true
	if err := r.recordUsage(line); err != nil {
		return true, err
	}
	if line.IsError || (line.Subtype != "" && line.Subtype != "success") {
		msg := line.Result
		if msg == "" {
			msg = strings.Join(line.Errors, "; ")
		}
		return true, errors.New(strings.TrimSpace("Claude Code: " + msg))
	}
	// A message steered in as the model finished runs as a
	// follow-up; wait for that one too.
	r.stopSteer()
	if r.sent.pending() > 0 {
		// The follow-up is still part of this Crush turn. Keep
		// forwarding new prompts while Claude works on it.
		r.stopSteer = r.startSteer()
		r.steerWait = time.After(steerEchoTimeout)
		return false, nil
	}
	r.finished = true
	return true, nil
}

// lastLine is the last non-blank line of text, trimmed.
func lastLine(text string) string {
	text = strings.TrimRight(text, " \t\r\n")
	if i := strings.LastIndexByte(text, '\n'); i >= 0 {
		text = text[i+1:]
	}
	return strings.TrimSpace(text)
}

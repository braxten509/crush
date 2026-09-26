package cliagent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"charm.land/fantasy"
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

func runClaude(ctx context.Context, m *Model, t Turn) error {
	args := []string{
		"-p", "--input-format", "stream-json", "--output-format", "stream-json",
		"--verbose", "--include-partial-messages", "--permission-prompt-tool", "stdio",
		"--model", m.ID,
	}
	if t.Effort != "" {
		args = append(args, "--effort", t.Effort)
	}
	if t.Resume != "" {
		args = append(args, "--resume", t.Resume)
	}
	if t.NoTools {
		args = append(args, "--tools", "", "--no-session-persistence", "--strict-mcp-config")
	}
	p, err := startProc(m.Dir, "claude", args...)
	if err != nil {
		return err
	}
	defer p.finish()
	stop := p.watchCancel(ctx, func() {
		_ = p.send(map[string]any{"type": "control_request", "request_id": "crush-interrupt", "request": map[string]any{"subtype": "interrupt"}})
	})
	defer stop()

	// A CLI that dies right away fails these writes; the read loop below
	// then reports why.
	_ = p.send(map[string]any{"type": "control_request", "request_id": "crush-init", "request": map[string]any{"subtype": "initialize"}})
	_ = p.send(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": t.Prompt}})

	// Crush tool name and input per tool_use ID, for results and approvals.
	calls := map[string][2]string{}
	started := false
	for p.lines.Scan() {
		var line claudeLine
		if json.Unmarshal(p.lines.Bytes(), &line) != nil {
			continue
		}
		switch line.Type {
		case "system":
			if line.Subtype == "init" && line.SessionID != "" {
				started = true
				if err := t.Emit(Event{Type: EventSession, Session: line.SessionID}); err != nil {
					return err
				}
			}

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
			return nil
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if !started && t.Resume != "" {
		return ErrResume
	}
	return exitError("claude", p)
}

func claudeStreamEvent(ev *claudeEvent, emit func(Event) error) error {
	if ev == nil {
		return nil
	}
	switch ev.Type {
	case "content_block_start":
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
	respond(map[string]any{"behavior": "deny", "message": "The user denied this tool call."})
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

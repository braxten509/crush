package cliagent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"

	"charm.land/fantasy"

	"github.com/charmbracelet/crush/internal/agent/tools/shelltool"
	"github.com/charmbracelet/crush/internal/message"
)

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
		msg = shelltool.SleepRefusal
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

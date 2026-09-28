package cliagent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/charmbracelet/crush/internal/agent/tools"
)

// Abacus (`abacusai`) speaks a Claude-like stream-json protocol: control
// requests for permissions, and its own events wrapped in {"type":"event"}.
type abacusLine struct {
	Type      string          `json:"type"`
	Subtype   string          `json:"subtype"`
	RequestID string          `json:"request_id"`
	Request   json.RawMessage `json:"request"`
	Result    string          `json:"result"`
	IsError   bool            `json:"is_error"`
	Event     struct {
		Type           string     `json:"type"`
		Content        string     `json:"content"`
		ConversationID string     `json:"conversationId"`
		ToolCallID     string     `json:"toolCallId"`
		ToolCall       abacusCall `json:"toolCall"`
		Result         struct {
			ToolCallID string `json:"toolCallId"`
			Output     string `json:"output"`
		} `json:"result"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	} `json:"event"`
}

type abacusCall struct {
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	Endpoint string         `json:"endpoint"`
	Args     map[string]any `json:"args"`
}

func runAbacus(ctx context.Context, m *Model, t Turn) error {
	args := []string{"-p", "--input-format", "stream-json", "--output-format", "stream-json", "--include-partial-messages", "--verbose"}
	if m.ID != "" {
		args = append(args, "--model", m.ID)
	}
	if t.Resume != "" {
		args = append(args, "--resume", t.Resume)
	}
	p, err := startProcEnv(m.Dir, t.Env, "abacusai", args...)
	if err != nil {
		return err
	}
	defer p.finish()
	stop := p.watchCancel(ctx, func() {
		_ = p.send(map[string]any{"type": "control_request", "request_id": "interrupt", "request": map[string]any{"subtype": "interrupt"}})
	})
	defer stop()
	_ = p.send(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": withImagePaths(t.Prompt, t.Attachments)}})

	// Abacus queues messages written mid-turn and hands them to the model
	// at its next step, reporting user_message_dequeued. Unconfirmed ones
	// are run by Crush after the turn.
	var sent steered
	stopSteer := pollSteer(t, func() bool { return true }, func(text string) {
		sent.add(text)
		if p.send(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": text}}) != nil {
			sent.take(text)
		}
	})
	defer stopSteer()

	open := map[string][2]string{} // running calls: Crush name, input
	var order []string
	read := map[string]bool{} // reads that finished before their call was announced
	seen := map[string]bool{}
	session, lastErr, emitted := "", "", false

	result := func(id, out string) error {
		call, ok := open[id]
		if !ok {
			return nil
		}
		delete(open, id)
		return t.Emit(Event{Type: EventToolResult, ID: id, Name: call[0], Output: out, Metadata: diskMetadata(call[0], call[1], out)})
	}
	// Abacus only reports results for some tools; the rest are done once
	// the model talks again or the turn ends.
	closeAll := func() error {
		for _, id := range order {
			if err := result(id, ""); err != nil {
				return err
			}
		}
		order = order[:0]
		return nil
	}

	for p.lines.Scan() {
		var line abacusLine
		if json.Unmarshal(p.lines.Bytes(), &line) != nil {
			continue
		}
		switch line.Type {
		case "control_request":
			go abacusControl(ctx, m, t, p, line)
		case "result":
			stopSteer()
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err := closeAll(); err != nil {
				return err
			}
			if line.IsError {
				if t.Resume != "" && !emitted {
					return ErrResume
				}
				return errors.New("Abacus: " + cmpOr(line.Result, lastErr, "turn failed"))
			}
			return nil
		case "event":
			e := line.Event
			switch e.Type {
			case "conversation_info":
				if e.ConversationID != "" && e.ConversationID != session {
					session = e.ConversationID
					if err := t.Emit(Event{Type: EventSession, Session: session}); err != nil {
						return err
					}
				}
			case "text_delta", "thinking_delta":
				if e.Content == "" {
					continue
				}
				emitted = true
				if err := closeAll(); err != nil {
					return err
				}
				typ := EventText
				if e.Type == "thinking_delta" {
					typ = EventReasoning
				}
				if err := t.Emit(Event{Type: typ, Text: e.Content}); err != nil {
					return err
				}
			case "tool_call":
				c := e.ToolCall
				if seen[c.ID] || c.ID == "" {
					continue
				}
				seen[c.ID] = true
				emitted = true
				name, input := abacusTool(c)
				open[c.ID] = [2]string{name, input}
				order = append(order, c.ID)
				if err := t.Emit(Event{Type: EventToolStart, ID: c.ID, Name: name}); err != nil {
					return err
				}
				if err := t.Emit(Event{Type: EventToolCall, ID: c.ID, Name: name, Input: input}); err != nil {
					return err
				}
				if read[c.ID] {
					if err := result(c.ID, ""); err != nil {
						return err
					}
				}
			case "file_read":
				read[e.ToolCallID] = true
				if err := result(e.ToolCallID, ""); err != nil {
					return err
				}
			case "tool_result":
				if err := result(e.Result.ToolCallID, e.Result.Output); err != nil {
					return err
				}
			case "file_write_done":
				if err := result(e.ToolCallID, ""); err != nil {
					return err
				}
			case "user_message_dequeued":
				for _, text := range sent.takeIn(e.Content) {
					if err := t.Emit(Event{Type: EventUserMessage, Text: text}); err != nil {
						return err
					}
				}
			case "error":
				lastErr = e.Error.Message
			case "turn_complete":
				if err := closeAll(); err != nil {
					return err
				}
			}
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return exitError("abacusai", p)
}

// abacusTool maps an Abacus tool call onto the matching Crush tool.
func abacusTool(c abacusCall) (string, string) {
	str := func(k string) string { s, _ := c.Args[k].(string); return s }
	path := cmpOr(str("path"), str("file_path"))
	switch {
	case c.Name == "bash" || c.Endpoint == "bash" || str("command") != "":
		return tools.BashToolName, marshal(map[string]string{"command": str("command"), "description": str("description")})
	case c.Endpoint == "file_read" || c.Name == "read":
		return tools.ViewToolName, marshal(map[string]string{"file_path": path})
	case strings.HasPrefix(c.Endpoint, "file_") && path != "":
		if content := str("content"); content != "" && str("old_str") == "" {
			return tools.WriteToolName, marshal(tools.WriteParams{FilePath: path, Content: content})
		}
		return tools.EditToolName, marshal(tools.EditParams{FilePath: path, OldString: str("old_str"), NewString: str("new_str")})
	}
	if c.Args == nil {
		return c.Name, "{}"
	}
	return c.Name, marshal(c.Args)
}

// abacusControl answers a permission request through Crush's dialog.
func abacusControl(ctx context.Context, m *Model, t Turn, p *proc, line abacusLine) {
	var req struct {
		Subtype    string `json:"subtype"`
		ToolCallID string `json:"tool_call_id"`
		Request    struct {
			Tool abacusCall `json:"tool"`
		} `json:"request"`
	}
	_ = json.Unmarshal(line.Request, &req)
	if req.Subtype != "can_use_tool" {
		return
	}
	name, input := abacusTool(req.Request.Tool)
	decision := "reject"
	if !t.NoTools && m.approve(ctx, t.SessionID, req.ToolCallID, name, input) {
		decision = "accept"
	}
	_ = p.send(map[string]any{"type": "control_response", "response": map[string]any{
		"subtype": "success", "request_id": line.RequestID, "response": map[string]any{"decision": decision},
	}})
}

package cliagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/version"
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
		Last struct {
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
)

func runCodex(ctx context.Context, m *Model, t Turn) error {
	p, err := startProc(m.Dir, "codex", "app-server")
	if err != nil {
		return err
	}
	defer p.finish()

	// Thread and turn IDs, read by the cancel watcher.
	var active atomic.Pointer[[2]string]
	stop := p.watchCancel(ctx, func() {
		if ids := active.Load(); ids != nil {
			_ = p.send(map[string]any{"id": "4", "method": "turn/interrupt", "params": map[string]any{"threadId": ids[0], "turnId": ids[1]}})
		} else {
			p.closeInput()
		}
	})
	var threadID string
	defer stop()

	// Messages queued mid-turn go in through turn/steer; Codex reports each
	// as a userMessage item when the model takes it in.
	var sent steered
	steers := 0
	stopSteer := pollSteer(t, func() bool { return active.Load() != nil }, func(text string) {
		ids := active.Load()
		steers++
		sent.add(text)
		err := p.send(map[string]any{"id": "steer-" + strconv.Itoa(steers), "method": "turn/steer", "params": map[string]any{
			"threadId":       ids[0],
			"expectedTurnId": ids[1],
			"input":          []any{map[string]any{"type": "text", "text": text, "text_elements": []any{}}},
		}})
		if err != nil {
			sent.take(text)
		}
	})
	defer stopSteer()

	// Crush's permission prompts replace Codex's own; the sandbox still
	// applies. YOLO mode lifts both, matching Crush's own tools.
	approval, sandbox := "on-request", "workspace-write"
	if t.NoTools {
		approval, sandbox = "never", "read-only"
	} else if m.Perms != nil && m.Perms.SkipRequests() {
		approval, sandbox = "never", "danger-full-access"
	}
	_ = p.send(map[string]any{"id": codexInitID, "method": "initialize", "params": map[string]any{
		"clientInfo": map[string]any{"name": "crush", "title": "Crush", "version": version.Version},
	}})

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
		}
		return t.Emit(Event{Type: EventToolResult, ID: id, Name: call[0], Output: out, Metadata: meta, IsError: isError})
	}

	for p.lines.Scan() {
		var msg rpcMessage
		if json.Unmarshal(p.lines.Bytes(), &msg) != nil {
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

		// Responses to our requests.
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
				params := map[string]any{"cwd": m.Dir, "model": m.ID, "approvalPolicy": approval, "sandbox": sandbox}
				method := "thread/start"
				if t.Resume != "" {
					method, params["threadId"] = "thread/resume", t.Resume
				} else if t.NoTools {
					params["ephemeral"] = true
				}
				_ = p.send(map[string]any{"id": codexThreadID, "method": method, "params": params})
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
				params := map[string]any{
					"threadId": threadID,
					"input":    []any{map[string]any{"type": "text", "text": t.Prompt, "text_elements": []any{}}},
				}
				if t.Effort != "" {
					params["effort"] = t.Effort
				}
				_ = p.send(map[string]any{"id": codexTurnID, "method": "turn/start", "params": params})
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
		_ = json.Unmarshal(msg.Params, &params)
		item := params.Item
		var err error
		switch msg.Method {
		case "item/agentMessage/delta":
			textOpen = true
			err = t.Emit(Event{Type: EventText, Text: params.Delta})
		case "item/reasoning/summaryTextDelta", "item/reasoning/textDelta":
			err = t.Emit(Event{Type: EventReasoning, Text: params.Delta})
		case "item/commandExecution/outputDelta":
			if output[params.ItemID] == nil {
				output[params.ItemID] = &strings.Builder{}
			}
			output[params.ItemID].WriteString(params.Delta)
		case "thread/tokenUsage/updated":
			if u := params.TokenUsage; u != nil {
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
			switch params.Turn.Status {
			case "completed":
				return nil
			case "interrupted":
				return context.Canceled
			}
			if e := params.Turn.Error; e != nil {
				return errors.New("Codex: " + e.Message)
			}
			return errors.New("Codex: turn " + params.Turn.Status)

		case "item/started":
			switch item.Type {
			case "userMessage":
				var text strings.Builder
				for _, c := range item.Content {
					text.WriteString(c.Text)
				}
				if sent.take(text.String()) {
					err = t.Emit(Event{Type: EventUserMessage, Text: text.String()})
				}
			case "agentMessage":
				// Consecutive messages without tools in between would run
				// together.
				if textOpen {
					err = t.Emit(Event{Type: EventText, Text: "\n\n"})
				}
			case "commandExecution":
				err = emitCall(item.ID, tools.BashToolName, marshal(map[string]string{"command": unwrapShell(item.Command), "description": ""}))
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
			case "commandExecution":
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
				for i := range changes[item.ID] {
					if err = emitResult(item.ID+"#"+strconv.Itoa(i), out, failed); err != nil {
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
	return exitError("codex", p)
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

var shellWrapper = regexp.MustCompile(`^\S*sh -l?c (?:'(.*)'|"(.*)")$`)

// unwrapShell turns Codex's `/bin/zsh -lc 'cmd'` into `cmd` for display.
func unwrapShell(cmd string) string {
	m := shellWrapper.FindStringSubmatch(cmd)
	switch {
	case m == nil:
		return cmd
	case m[2] != "":
		return strings.NewReplacer(`\"`, `"`, `\\`, `\`, `\$`, `$`, "\\`", "`").Replace(m[2])
	}
	return strings.ReplaceAll(m[1], `'\''`, `'`)
}

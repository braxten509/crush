package cliagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/agent/tools"
)

// AGY (Antigravity CLI) streams step updates as JSON lines in print mode.
// It has no permission bridge; it follows its own permission mode.
type agyLine struct {
	Event          string `json:"event"`
	ConversationID string `json:"conversation_id"`
	StepUpdate     struct {
		StepIndex int    `json:"step_index"`
		State     string `json:"state"`
		StepType  string `json:"step_type"`
		TextDelta string `json:"text_delta"`
		Usage     *struct {
			InputTokens     int64 `json:"input_tokens"`
			OutputTokens    int64 `json:"output_tokens"`
			ThinkingTokens  int64 `json:"thinking_tokens"`
			CacheReadTokens int64 `json:"cache_read_tokens"`
		} `json:"usage"`
		ToolName string `json:"tool_name"`
		ToolInfo struct {
			Parameters map[string]any `json:"parameters"`
			Output     string         `json:"output"`
			Error      *struct {
				Message string `json:"message"`
			} `json:"error"`
		} `json:"tool_info"`
	} `json:"step_update"`
	Result struct {
		Status   string `json:"status"`
		Response string `json:"response"`
	} `json:"result"`
}

// agyBin finds the real agy binary; shells often wrap it in a function
// that adds flags we don't want.
func agyBin() string {
	if home, err := os.UserHomeDir(); err == nil {
		if bin := filepath.Join(home, ".local", "bin", "agy"); isExec(bin) {
			return bin
		}
	}
	return "agy"
}

func isExec(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0
}

func runAGY(ctx context.Context, m *Model, t Turn) error {
	args := []string{"--print=", "--input-format", "stream-json", "--output-format", "stream-json", "--model", m.ID}
	if t.Resume != "" {
		args = append(args, "--conversation", t.Resume)
	}
	name := agyBin()
	if m.Guarded && !t.NoTools {
		// AGY runs every command without asking and can't hand them to
		// Crush, and its own sandbox falls back to running on the host.
		// Codex's sandbox keeps it to the project (plus its own state and
		// the network), which also rules out sudo.
		if _, err := exec.LookPath("codex"); err != nil {
			return errors.New("AGY sub-agents run inside Codex's sandbox, and codex isn't installed")
		}
		state := filepath.Join(os.Getenv("HOME"), ".gemini")
		args = append([]string{
			"sandbox",
			"-c", `sandbox_mode="workspace-write"`,
			"-c", "sandbox_workspace_write.network_access=true",
			"-c", fmt.Sprintf("sandbox_workspace_write.writable_roots=[%q]", state),
			"--", name,
		}, args...)
		name = "codex"
	}
	p, err := startProcEnv(m.Dir, t.Env, name, args...)
	if err != nil {
		return err
	}
	defer p.finish()
	// Print mode has no interrupt message, but SIGINT stops the turn.
	stop := p.watchCancel(ctx, func() {
		if p.cmd.Process.Signal(os.Interrupt) != nil {
			p.closeInput()
		}
	})
	defer stop()
	_ = p.send(map[string]any{"event": "user", "message": map[string]any{"role": "user", "content": withImagePaths(t.Prompt, t.Attachments)}})

	announced := map[int]bool{}
	for p.lines.Scan() {
		if ctx.Err() != nil {
			// Interrupted: let agy stop its commands and exit by itself;
			// closing its input early cuts that short.
			continue
		}
		var line agyLine
		if json.Unmarshal(p.lines.Bytes(), &line) != nil {
			continue
		}
		switch line.Event {
		case "init":
			// An unknown --conversation silently starts a new one.
			if t.Resume != "" && line.ConversationID != t.Resume {
				return ErrResume
			}
			if err := t.Emit(Event{Type: EventSession, Session: line.ConversationID}); err != nil {
				return err
			}
		case "step_update":
			if err := agyStep(line, announced, t.Emit); err != nil {
				return err
			}
		case "result":
			if line.Result.Status != "SUCCESS" {
				return errors.New("AGY: " + cmpOr(line.Result.Response, "turn failed"))
			}
			return nil
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return exitError("agy", p)
}

func agyStep(line agyLine, announced map[int]bool, emit func(Event) error) error {
	s := line.StepUpdate
	switch s.StepType {
	case "agent_response":
		if s.TextDelta != "" {
			if err := emit(Event{Type: EventText, Text: s.TextDelta}); err != nil {
				return err
			}
		}
		if u := s.Usage; u != nil && s.State == "DONE" {
			return emit(Event{Type: EventUsage, Usage: fantasy.Usage{
				InputTokens: u.InputTokens - u.CacheReadTokens, OutputTokens: u.OutputTokens,
				CacheReadTokens: u.CacheReadTokens, ReasoningTokens: u.ThinkingTokens,
			}})
		}
	case "tool":
		id := "agy-" + strconv.Itoa(s.StepIndex)
		name, input := agyTool(s.ToolName, s.ToolInfo.Parameters)
		if !announced[s.StepIndex] {
			announced[s.StepIndex] = true
			if err := emit(Event{Type: EventToolStart, ID: id, Name: name}); err != nil {
				return err
			}
			if err := emit(Event{Type: EventToolCall, ID: id, Name: name, Input: input}); err != nil {
				return err
			}
		}
		switch s.State {
		case "DONE":
			return emit(Event{Type: EventToolResult, ID: id, Name: name, Output: s.ToolInfo.Output, Metadata: diskMetadata(name, input, s.ToolInfo.Output)})
		case "ERROR":
			msg := "failed"
			if s.ToolInfo.Error != nil {
				msg = s.ToolInfo.Error.Message
			}
			return emit(Event{Type: EventToolResult, ID: id, Name: name, Output: msg, IsError: true})
		}
	}
	return nil
}

// agyTool maps AGY's tools onto Crush's. AGY reports paths but not file
// contents, so edits carry only the path and the diff comes from disk.
func agyTool(name string, params map[string]any) (string, string) {
	str := func(k string) string { s, _ := params[k].(string); return s }
	switch name {
	case "run_command":
		return tools.BashToolName, marshal(map[string]string{"command": str("CommandLine"), "description": ""})
	case "view_file":
		return tools.ViewToolName, marshal(map[string]string{"file_path": str("AbsolutePath")})
	case "replace_file_content", "multi_replace_file_content", "write_to_file", "sed_file":
		return tools.EditToolName, marshal(map[string]string{"file_path": str("TargetFile")})
	}
	if params == nil {
		return name, "{}"
	}
	return name, marshal(params)
}

// diskMetadata is resultMetadata for CLIs that don't return file contents:
// views read the file itself. Edit diffs are filled in by the caller from
// before/after snapshots.
func diskMetadata(name, input, output string) string {
	switch name {
	case tools.ViewToolName:
		var in struct {
			FilePath string `json:"file_path"`
		}
		_ = json.Unmarshal([]byte(input), &in)
		content, _ := os.ReadFile(in.FilePath)
		return marshal(tools.ViewResponseMetadata{FilePath: in.FilePath, Content: clipBytes(content, 64<<10)})
	case tools.EditToolName, tools.WriteToolName:
		return ""
	}
	return resultMetadata(name, input, output)
}

func clipBytes(b []byte, max int) string {
	if len(b) > max {
		b = b[:max]
	}
	return string(b)
}

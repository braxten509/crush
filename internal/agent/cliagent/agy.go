package cliagent

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"time"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/message"
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
		// Error is why a failed turn stopped ("Individual quota reached
		// ... Resets in 4h52m"); Response then holds only the last text.
		Error string `json:"error"`
		// FinishedReason says why a turn ended early ("error",
		// "interrupted", "print_timeout"); empty when it finished.
		FinishedReason string `json:"finished_reason"`
	} `json:"result"`
}

// agyBackgroundNote is the result shown for a command AGY is still running
// once it has held the turn for the user's time. AGY moves a slow command
// to its own background after a few seconds and its model carries on, but
// print mode reports steps in order: nothing past the command reaches Crush
// until it ends.
const agyBackgroundNote = "Still running. AGY moved this command to its own background and carries on when it finishes."

// agyTurn is what one AGY turn has reported so far.
type agyTurn struct {
	emit      func(Event) error
	announced map[int]bool
	// moved are the commands shown as running in the background; their own
	// results are not shown again.
	moved map[int]bool
	// replied is set while the last step reported is a finished reply.
	replied bool

	mu sync.Mutex
	// running are the commands AGY has not finished, by step index: the
	// tool's name and input.
	running map[int][2]string
}

// waiting reports whether AGY is still running a command of this turn.
func (r *agyTurn) waiting() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.running) > 0
}

// moveToBackground shows the commands AGY is still running as moved to the
// background, the way Ctrl+B does for the other CLIs.
func (r *agyTurn) moveToBackground() error {
	r.mu.Lock()
	calls := r.running
	r.running = map[int][2]string{}
	r.mu.Unlock()
	for _, index := range slices.Sorted(maps.Keys(calls)) {
		call := calls[index]
		r.moved[index] = true
		meta := markBackground(call[0], resultMetadata(call[0], call[1], agyBackgroundNote))
		if err := r.emit(Event{Type: EventToolResult, ID: "agy-" + strconv.Itoa(index), Name: call[0], Output: agyBackgroundNote, Metadata: meta}); err != nil {
			return err
		}
	}
	return nil
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

// agyFollowUpWait is how long a finished turn waits for AGY to start on a
// line steered into it. AGY may have folded the line into the turn, so a
// quiet wait ends it.
var agyFollowUpWait = 15 * time.Second

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
	if m.Guarded && !m.ReadOnly && !t.NoTools {
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
	p, err := m.startTurnProc(t.NoTools, t.Env, name, args...)
	if err != nil {
		return err
	}
	defer p.finish()
	t.Emit = p.reviewEvents(t.Emit)
	// Print mode has no interrupt message, but SIGINT stops the turn.
	stop := p.watchCancel(ctx, func() {
		if p.cmd.Process.Signal(os.Interrupt) != nil {
			p.closeInput()
		}
	})
	defer stop()
	_ = p.send(map[string]any{"event": "user", "message": map[string]any{"role": "user", "content": withImagePaths(t.Prompt, t.Attachments)}})

	// A message queued mid-turn goes in as another user line; AGY runs it
	// as its next turn in this conversation (owner, 2026-10-24). It stays
	// listed as queued until the turn before it ends: shown at once, the
	// chat read "Waiting for the model" while AGY's command still ran.
	var (
		steerMu sync.Mutex
		steered []string
	)
	startSteer := func() func() {
		return pollSteerInputRetry(t, func() bool { return true }, func(text string, attachments []message.Attachment) bool {
			if p.send(map[string]any{"event": "user", "message": map[string]any{"role": "user", "content": withImagePaths(text, attachments)}}) != nil {
				return false
			}
			steerMu.Lock()
			steered = append(steered, text)
			steerMu.Unlock()
			return true
		})
	}
	stopSteer := startSteer()
	defer func() { stopSteer() }()

	run := &agyTurn{emit: t.Emit, announced: map[int]bool{}, moved: map[int]bool{}, running: map[int][2]string{}}
	// Ctrl+B and the user's background time: asked from another goroutine,
	// done here between lines.
	toBackground := make(chan struct{}, 1)
	if t.SessionID != "" {
		defer onBackground(t.SessionID, func() bool {
			if !run.waiting() {
				return false
			}
			select {
			case toBackground <- struct{}{}:
			default:
			}
			return true
		})()
	}
	// The model's context window, for the context meter.
	var windows <-chan int64
	if !t.NoTools {
		size, asked := agyContextWindow(ctx, m.ID)
		if size > 0 {
			if err := t.Emit(agyContextBudget(size)); err != nil {
				return err
			}
		}
		windows = asked
	}
	conversation := t.Resume
	lines := p.readLines()
	// followUp is armed after a result while steered lines may still run
	// as AGY's next turns; any new line disarms it.
	var followUp <-chan time.Time
	results := 0
	for {
		var raw []byte
		select {
		case l, ok := <-lines:
			if !ok {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				return exitError("agy", p)
			}
			raw = l
			followUp = nil
		case <-followUp:
			return nil
		case <-toBackground:
			if err := run.moveToBackground(); err != nil {
				return err
			}
			continue
		case size := <-windows:
			windows = nil
			if err := t.Emit(agyContextBudget(size)); err != nil {
				return err
			}
			continue
		}
		if ctx.Err() != nil {
			// Interrupted: let agy stop its commands and exit by itself;
			// closing its input early cuts that short.
			continue
		}
		var line agyLine
		if json.Unmarshal(raw, &line) != nil {
			continue
		}
		switch line.Event {
		case "init":
			// An unknown --conversation silently starts a new one.
			if t.Resume != "" && line.ConversationID != t.Resume {
				return ErrResume
			}
			conversation = line.ConversationID
			if err := t.Emit(Event{Type: EventSession, Session: line.ConversationID}); err != nil {
				return err
			}
		case "step_update":
			// AGY's plan is read over HTTP, so its stream carries no new
			// usage: read it as each tool step runs, not only at the end.
			if line.StepUpdate.StepType == "tool" {
				RefreshLimitsDuringTurn(ctx, m.Kind, m.Account)
			}
			if err := run.step(line); err != nil {
				return err
			}
			if err := agyMarker(line, conversation, t); err != nil {
				return err
			}
		case "result":
			// The turn's last reply has counted by now.
			RefreshLimitsDuringTurn(ctx, m.Kind, m.Account)
			if line.Result.Status != "SUCCESS" {
				// Once AGY has recovered from a failed step, it reports
				// that step's error as the result of every later turn of
				// the conversation, finished or not (agy 1.5.9). A turn
				// that ended on a finished reply did not fail.
				stale := run.replied && (line.Result.FinishedReason == "" || line.Result.FinishedReason == "error")
				if !stale {
					return errors.New("AGY: " + cmp.Or(line.Result.Error, line.Result.Response, "turn failed"))
				}
				slog.Debug("AGY reported an earlier step's error for a finished turn", "error", line.Result.Error)
			}
			run.replied = false
			// Each steered line may run as one more turn with its own
			// result. Stop steering first so the count is final.
			results++
			stopSteer()
			steerMu.Lock()
			next := ""
			if results <= len(steered) {
				next = steered[results-1]
			}
			steerMu.Unlock()
			if next == "" {
				return nil
			}
			// AGY starts on the next steered line now.
			if err := t.Emit(Event{Type: EventUserMessage, Text: next}); err != nil {
				return err
			}
			stopSteer = startSteer()
			followUp = time.After(agyFollowUpWait)
		}
	}
}

func (r *agyTurn) step(line agyLine) error {
	s := line.StepUpdate
	emit, announced := r.emit, r.announced
	r.replied = s.StepType == "agent_response" && s.State == "DONE"
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
			if name == tools.BashToolName && s.State != "DONE" && s.State != "ERROR" {
				r.mu.Lock()
				r.running[s.StepIndex] = [2]string{name, input}
				r.mu.Unlock()
			}
		}
		if s.State == "DONE" || s.State == "ERROR" {
			r.mu.Lock()
			delete(r.running, s.StepIndex)
			r.mu.Unlock()
			if r.moved[s.StepIndex] {
				// Already shown as running in the background; AGY's model
				// reads the output and says what came of it.
				return nil
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

// agyMarker reports the step index of a finished step: a reply on the reply
// message, a tool on its result. AGY always resumes through the written
// copy (Q3), so the marker is only recorded.
func agyMarker(line agyLine, conversation string, t Turn) error {
	s := line.StepUpdate
	if !t.marks() || conversation == "" {
		return nil
	}
	target := ""
	switch {
	case s.StepType == "agent_response" && s.State == "DONE":
		target = MarkerForAssistant
	case s.StepType == "tool" && (s.State == "DONE" || s.State == "ERROR"):
		target = MarkerForTool
	default:
		return nil
	}
	return t.Emit(Event{Type: EventMarker, Marker: &Marker{Native: conversation, ID: strconv.Itoa(s.StepIndex), Kind: MarkerStep, For: target}})
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
	// Newer AGY names (edit_file, read_file) by their words and fields.
	switch path := inputField(params, pathFields...); toolKind("", name) {
	case "execute":
		if cmd := inputField(params, commandFields...); cmd != "" {
			return tools.BashToolName, marshal(map[string]string{"command": cmd, "description": ""})
		}
	case "read":
		if path != "" {
			return tools.ViewToolName, marshal(map[string]string{"file_path": path})
		}
	case "edit":
		if path != "" {
			return tools.EditToolName, marshal(map[string]string{"file_path": path})
		}
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

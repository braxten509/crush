// Package cliagent runs turns through installed agent CLIs (Claude Code,
// Codex, Grok, OpenCode, AGY, Abacus) instead of an HTTP API. Each CLI
// keeps its own tools, login and settings; this package speaks its
// streaming protocol and turns what it does into [Event]s that Crush
// renders like any other turn.
package cliagent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"charm.land/catwalk/pkg/catwalk"
	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/history"
	"github.com/charmbracelet/crush/internal/permission"
)

// ErrResume means the CLI could not reopen the native session it was asked
// to resume (deleted, expired or from another machine).
var ErrResume = errors.New("native session could not be resumed")

// EventType identifies what an [Event] carries.
type EventType int

const (
	EventText        EventType = iota // Text
	EventReasoning                    // Text
	EventToolStart                    // ID, Name
	EventToolCall                     // ID, Name, Input
	EventToolResult                   // ID, Name, Output, Metadata, IsError
	EventUsage                        // Usage (per model request)
	EventSession                      // Session (native session ID)
	EventUserMessage                  // Text (a steered message the CLI took in)
)

// Event is one normalized piece of a CLI turn. Tool names and inputs are
// already translated to Crush's own tools where one matches, so the UI can
// use its native renderers.
type Event struct {
	Type     EventType
	Text     string
	ID       string
	Name     string
	Input    string
	Output   string
	Metadata string
	IsError  bool
	Usage    fantasy.Usage
	Session  string
}

// Turn is one user prompt sent to a CLI.
type Turn struct {
	SessionID string // Crush session, used for permission prompts
	Prompt    string
	Resume    string // native session to continue; empty starts a new one
	Effort    string
	// NoTools runs a one-shot, text-only request that is not saved as a
	// native session (titles, summaries).
	NoTools bool
	Emit    func(Event) error
	// Steer returns messages the user queued while the turn runs, or "".
	// Drivers whose CLI can take input mid-turn poll it, send what it
	// returns and emit EventUserMessage with the same text once the CLI
	// takes it in. Anything never confirmed is Crush's to run later.
	Steer func() string
}

// Model is a CLI-backed model. It implements [fantasy.LanguageModel] for
// one-shot text requests; full agent turns go through [Model.Run].
type Model struct {
	Kind  catwalk.Type
	ID    string
	Dir   string
	Perms permission.Service
	Files history.Service
	Links *Links
}

// NewProvider returns a [fantasy.Provider] whose models run through the
// agent CLI of the given kind.
func NewProvider(kind catwalk.Type, dir, dataDir string, perms permission.Service, files history.Service) fantasy.Provider {
	return &provider{kind: kind, dir: dir, perms: perms, files: files, links: &Links{path: filepath.Join(dataDir, "cli-sessions.json")}}
}

type provider struct {
	kind  catwalk.Type
	dir   string
	perms permission.Service
	files history.Service
	links *Links
}

func (p *provider) Name() string { return string(p.kind) }

func (p *provider) LanguageModel(_ context.Context, modelID string) (fantasy.LanguageModel, error) {
	return &Model{Kind: p.kind, ID: modelID, Dir: p.dir, Perms: p.perms, Files: p.files, Links: p.links}, nil
}

// Run executes one turn, emitting events until the CLI finishes it.
func (m *Model) Run(ctx context.Context, t Turn) error {
	switch m.Kind {
	case config.TypeClaudeCode:
		return runClaude(ctx, m, t)
	case config.TypeCodexCLI:
		return runCodex(ctx, m, t)
	case config.TypeGrokCLI:
		args := []string{"agent", "--no-leader", "-m", m.ID}
		if t.Effort != "" {
			args = append(args, "--reasoning-effort", t.Effort)
		}
		return runACP(ctx, m, t, "grok", append(args, "stdio"), false)
	case config.TypeOpenCodeCLI:
		return runACP(ctx, m, t, "opencode", []string{"acp"}, true)
	case config.TypeAGYCLI:
		return runAGY(ctx, m, t)
	case config.TypeAbacusCLI:
		return runAbacus(ctx, m, t)
	}
	return fmt.Errorf("unknown agent CLI %q", m.Kind)
}

func (m *Model) Provider() string { return string(m.Kind) }
func (m *Model) Model() string    { return m.ID }

// Generate runs a text-only request, used for titles and summaries.
func (m *Model) Generate(ctx context.Context, call fantasy.Call) (*fantasy.Response, error) {
	var text strings.Builder
	var usage fantasy.Usage
	err := m.Run(ctx, Turn{Prompt: flattenPrompt(call.Prompt), NoTools: true, Emit: func(e Event) error {
		switch e.Type {
		case EventText:
			text.WriteString(e.Text)
		case EventUsage:
			usage = e.Usage
		}
		return nil
	}})
	if err != nil {
		return nil, err
	}
	return &fantasy.Response{
		Content:      fantasy.ResponseContent{fantasy.TextContent{Text: text.String()}},
		FinishReason: fantasy.FinishReasonStop,
		Usage:        usage,
	}, nil
}

// Stream replays [Model.Generate] as a stream.
// ponytail: not incremental; only titles and summaries use it.
func (m *Model) Stream(ctx context.Context, call fantasy.Call) (fantasy.StreamResponse, error) {
	resp, err := m.Generate(ctx, call)
	if err != nil {
		return nil, err
	}
	return func(yield func(fantasy.StreamPart) bool) {
		_ = yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextStart, ID: "0"}) &&
			yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextDelta, ID: "0", Delta: resp.Content.Text()}) &&
			yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextEnd, ID: "0"}) &&
			yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeFinish, FinishReason: resp.FinishReason, Usage: resp.Usage})
	}, nil
}

func (m *Model) GenerateObject(context.Context, fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	return nil, errors.New("structured output is not supported by agent CLIs")
}

func (m *Model) StreamObject(context.Context, fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	return nil, errors.New("structured output is not supported by agent CLIs")
}

// flattenPrompt renders fantasy messages as plain text for a one-shot CLI call.
func flattenPrompt(prompt fantasy.Prompt) string {
	var b strings.Builder
	for _, msg := range prompt {
		for _, part := range msg.Content {
			var text string
			switch p := part.(type) {
			case fantasy.TextPart:
				text = p.Text
			case fantasy.ReasoningPart:
				continue
			case fantasy.ToolCallPart:
				text = fmt.Sprintf("[tool call %s: %s]", p.ToolName, p.Input)
			case fantasy.ToolResultPart:
				if out, ok := p.Output.(fantasy.ToolResultOutputContentText); ok {
					text = "[tool result: " + out.Text + "]"
				}
			}
			if text == "" {
				continue
			}
			if b.Len() > 0 {
				b.WriteString("\n\n")
			}
			if msg.Role != fantasy.MessageRoleSystem {
				b.WriteString(string(msg.Role) + ": ")
			}
			b.WriteString(text)
		}
	}
	return b.String()
}

// Link ties a Crush session to a CLI's native session.
type Link struct {
	Native string `json:"native"`
	// Through is the last Crush message the native session has seen.
	// Anything after it happened elsewhere and is handed over on resume.
	Through string `json:"through"`
}

// Links persists [Link]s per Crush session and CLI.
// ponytail: one small JSON file rewritten per turn; a DB table if it grows.
type Links struct {
	path string
	mu   sync.Mutex
}

func (l *Links) load() map[string]Link {
	links := map[string]Link{}
	if data, err := os.ReadFile(l.path); err == nil {
		_ = json.Unmarshal(data, &links)
	}
	return links
}

// Get returns the link for a session and CLI, or the zero Link.
func (l *Links) Get(sessionID string, kind catwalk.Type) Link {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.load()[sessionID+"/"+string(kind)]
}

// Set stores the link for a session and CLI.
func (l *Links) Set(sessionID string, kind catwalk.Type, link Link) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	links := l.load()
	links[sessionID+"/"+string(kind)] = link
	data, err := json.Marshal(links)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(l.path), 0o700); err != nil {
		return err
	}
	tmp := l.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, l.path)
}

// pollSteer hands queued messages to send while the turn runs, whenever
// ready says the CLI can take one. The returned stop func is idempotent;
// once it returns, send is never called again.
func pollSteer(t Turn, ready func() bool, send func(text string)) (stop func()) {
	if t.Steer == nil || t.NoTools {
		return func() {}
	}
	var (
		mu      sync.Mutex
		stopped bool
	)
	done := make(chan struct{})
	go func() {
		tick := time.NewTicker(250 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-done:
				return
			case <-tick.C:
			}
			mu.Lock()
			if !stopped && ready() {
				if text := t.Steer(); text != "" {
					send(text)
				}
			}
			mu.Unlock()
		}
	}()
	return func() {
		mu.Lock()
		defer mu.Unlock()
		if !stopped {
			stopped = true
			close(done)
		}
	}
}

// steered tracks messages sent to a CLI mid-turn until it confirms them.
type steered struct {
	mu    sync.Mutex
	texts []string
}

func (s *steered) add(text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.texts = append(s.texts, text)
}

// take removes text if it was steered, reporting whether it was.
func (s *steered) take(text string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, t := range s.texts {
		if t == text {
			s.texts = append(s.texts[:i], s.texts[i+1:]...)
			return true
		}
	}
	return false
}

func (s *steered) pending() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.texts)
}

// proc is a CLI child process speaking newline-delimited JSON on stdio.
type proc struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	lines  *bufio.Scanner
	stderr *tailBuffer
	mu     sync.Mutex
	closed bool
}

func startProc(dir, name string, args ...string) (*proc, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	ownGroup(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	p := &proc{cmd: cmd, stdin: stdin, stderr: &tailBuffer{max: 8 << 10}}
	cmd.Stderr = p.stderr
	p.lines = bufio.NewScanner(stdout)
	p.lines.Buffer(make([]byte, 0, 1<<20), 64<<20)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting %s: %w", name, err)
	}
	return p, nil
}

func (p *proc) send(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return io.ErrClosedPipe
	}
	_, err = p.stdin.Write(append(data, '\n'))
	return err
}

// closeInput ends stdin, which both CLIs treat as "exit when done".
func (p *proc) closeInput() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.closed {
		p.closed = true
		_ = p.stdin.Close()
	}
}

// finish waits for the process, killing it if it lingers.
func (p *proc) finish() {
	p.closeInput()
	timer := time.AfterFunc(5*time.Second, p.kill)
	defer timer.Stop()
	if err := p.cmd.Wait(); err != nil {
		slog.Debug("Agent CLI exited", "cmd", p.cmd.Path, "error", err, "stderr", p.stderr.String())
	}
}

// watchCancel calls interrupt once ctx is done, then gives the CLI a few
// seconds to wind down before killing it. The returned func stops watching.
func (p *proc) watchCancel(ctx context.Context, interrupt func()) func() {
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			// Stop the commands the CLI is running first: some start them
			// detached, where stopping the CLI doesn't reach them.
			p.killCommands()
			interrupt()
			select {
			case <-time.After(5 * time.Second):
				p.closeInput()
				p.kill()
			case <-done:
			}
		case <-done:
		}
	}()
	return func() { close(done) }
}

// tailBuffer keeps the last max bytes written to it.
type tailBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
	max int
}

func (t *tailBuffer) Write(b []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf.Write(b)
	if over := t.buf.Len() - t.max; over > 0 {
		t.buf.Next(over)
	}
	return len(b), nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return strings.TrimSpace(t.buf.String())
}

// exitError explains a CLI that stopped without finishing the turn.
func exitError(name string, p *proc) error {
	if msg := p.stderr.String(); msg != "" {
		return fmt.Errorf("%s exited: %s", name, lastLines(msg, 5))
	}
	return fmt.Errorf("%s exited before finishing the turn", name)
}

func lastLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

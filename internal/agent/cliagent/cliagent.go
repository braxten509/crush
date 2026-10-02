// Package cliagent runs turns through installed agent CLIs (Claude Code,
// Codex, Grok, OpenCode, AGY) instead of an HTTP API. Each CLI
// keeps its own tools, login and settings; this package speaks its
// streaming protocol and turns what it does into [Event]s that Crush
// renders like any other turn.
package cliagent

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"charm.land/catwalk/pkg/catwalk"
	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/filechange"
	"github.com/charmbracelet/crush/internal/history"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/permission"
	"mvdan.cc/sh/v3/shell"
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
	EventCompacting                   // Compacting (native context compaction status)
)

// TextBreak is sent as text when a new text block starts. It separates two
// of the model's messages that land in one step and is dropped otherwise.
const TextBreak = "\n\n"

// Event is one normalized piece of a CLI turn. Tool names and inputs are
// already translated to Crush's own tools where one matches, so the UI can
// use its native renderers.
type Event struct {
	Compacting bool
	Type       EventType
	Text       string
	ID         string
	Name       string
	Input      string
	Output     string
	Metadata   string
	IsError    bool
	Usage      fantasy.Usage
	Session    string
}

// Turn is one user prompt sent to a CLI.
type Turn struct {
	SessionID string // Crush session, used for permission prompts
	Prompt    string
	// Continue shows a reply the CLI started on its own between turns
	// (see [OnUnprompted]) instead of sending Prompt.
	Continue    bool
	Resume      string // native session to continue; empty starts a new one
	Effort      string
	Attachments []message.Attachment
	// NoTools runs a one-shot, text-only request that is not saved as a
	// native session (titles, summaries).
	NoTools bool
	// System replaces Claude's own system prompt on NoTools requests.
	System string
	// Instructions are added to Claude's system prompt. Other CLIs have no
	// such option, so Crush puts them in the prompt instead.
	Instructions string
	// Env is added to the CLI process environment.
	Env  []string
	Emit func(Event) error
	// Steer returns messages the user queued while the turn runs, or "".
	// Drivers whose CLI can take input mid-turn poll it, send what it
	// returns and emit EventUserMessage with the same text once the CLI
	// takes it in. Anything never confirmed is Crush's to run later.
	Steer func() string
	// SteerInput preserves images for drivers with native multimodal steering.
	SteerInput func() (string, []message.Attachment)
	// SteerReady wakes the driver as soon as a prompt is queued. Polling
	// remains a fallback for prompts queued before the driver starts.
	SteerReady <-chan struct{}
}

// Model is a CLI-backed model. It implements [fantasy.LanguageModel] for
// one-shot text requests; full agent turns go through [Model.Run].
type Model struct {
	Kind        catwalk.Type
	ID          string
	ServiceTier string
	Dir         string
	Perms       permission.Service
	Files       history.Service
	Links       *Links
	// Guarded runs a background sub-agent: nobody is there to approve its
	// tool calls, so Crush answers them itself, granting whatever its own
	// YOLO mode allows. The CLI's bypass modes stay off so every call
	// still comes to Crush.
	Guarded bool
	// Env is added to the CLI process environment on every turn.
	Env []string
}

// NewProvider returns a [fantasy.Provider] whose models run through the
// agent CLI of the given kind.
func NewProvider(kind catwalk.Type, dir, dataDir string, perms permission.Service, files history.Service, serviceTier string) fantasy.Provider {
	return &provider{kind: kind, dir: dir, perms: perms, files: files, serviceTier: serviceTier, links: &Links{path: filepath.Join(dataDir, "cli-sessions.json")}}
}

type provider struct {
	kind        catwalk.Type
	serviceTier string
	dir         string
	perms       permission.Service
	files       history.Service
	links       *Links
}

func (p *provider) Name() string { return string(p.kind) }

func (p *provider) LanguageModel(_ context.Context, modelID string) (fantasy.LanguageModel, error) {
	return &Model{Kind: p.kind, ID: modelID, ServiceTier: p.serviceTier, Dir: p.dir, Perms: p.perms, Files: p.files, Links: p.links}, nil
}

// Run executes one turn, emitting events until the CLI finishes it.
func (m *Model) Run(ctx context.Context, t Turn) error {
	if t.Continue && m.Kind != config.TypeClaudeCode {
		return nil // the session moved on to another CLI
	}
	calls := &openCalls{calls: map[string]openCall{}}
	if t.Emit != nil {
		t.Emit = calls.track(t.Emit)
	}
	ctx = context.WithValue(ctx, openCallsKey{}, calls)
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
		// Crush hands every CLI the shared memory; Grok's own stays off.
		t.Env = append(slices.Clone(t.Env), "GROK_MEMORY=0")
		return runACP(ctx, m, t, "grok", append(args, "stdio"), false)
	case config.TypeOpenCodeCLI:
		return runACP(ctx, m, t, "opencode", []string{"acp"}, true)
	case config.TypeAGYCLI:
		return runAGY(ctx, m, t)
	}
	return fmt.Errorf("unknown agent CLI %q", m.Kind)
}

func (m *Model) Provider() string { return string(m.Kind) }
func (m *Model) Model() string    { return m.ID }

// Generate runs a text-only request, used for titles and summaries.
func (m *Model) Generate(ctx context.Context, call fantasy.Call) (*fantasy.Response, error) {
	var text strings.Builder
	var usage fantasy.Usage
	t := Turn{Prompt: flattenPrompt(call.Prompt), NoTools: true}
	if m.Kind == config.TypeClaudeCode {
		// A short system prompt of its own keeps these requests to a few
		// hundred tokens instead of Claude Code's whole agent setup.
		var sys, rest fantasy.Prompt
		for _, msg := range call.Prompt {
			if msg.Role == fantasy.MessageRoleSystem {
				sys = append(sys, msg)
			} else {
				rest = append(rest, msg)
			}
		}
		t.System, t.Prompt = flattenPrompt(sys), flattenPrompt(rest)
	}
	t.Emit = func(e Event) error {
		switch e.Type {
		case EventText:
			text.WriteString(e.Text)
		case EventUsage:
			usage = e.Usage
		}
		return nil
	}
	if err := m.Run(ctx, t); err != nil {
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
	// Tasks is set once the native session was told how to spawn
	// sub-agents.
	Tasks bool `json:"tasks,omitempty"`
	// SharedInstructions records delivery without invalidating the session.
	SharedInstructions bool `json:"shared_instructions,omitempty"`
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

// backgrounders holds, per Crush session, how its running turn moves the
// commands it waits on to the background ([Background]).
var backgrounders sync.Map // session ID -> *backgrounder

type backgrounder struct{ fn func() bool }

// onBackground registers fn as the session's Ctrl+B for the running turn;
// the returned func unregisters it.
func onBackground(sessionID string, fn func() bool) func() {
	b := &backgrounder{fn}
	backgrounders.Store(sessionID, b)
	return func() { backgrounders.CompareAndDelete(sessionID, b) }
}

// Background moves the commands the session's running turn is waiting on to
// the background, as Ctrl+B does in Claude Code. It reports whether the
// session's CLI could be asked: Claude, Codex and OpenCode can.
func Background(sessionID string) bool {
	v, ok := backgrounders.Load(sessionID)
	return ok && v.(*backgrounder).fn()
}

// CanBackground reports whether a CLI's running commands can be moved to
// the background with [Background].
func CanBackground(kind catwalk.Type) bool {
	return kind == config.TypeClaudeCode || kind == config.TypeCodexCLI || kind == config.TypeOpenCodeCLI
}

// OnUnprompted is called with a Crush session whose kept CLI process started
// a reply on its own between turns (a background task it ran finished). The
// caller runs a turn with [Turn.Continue] set to show it.
var OnUnprompted func(sessionID string)

// pollSteer hands queued messages to send while the turn runs, whenever
// ready says the CLI can take one. The returned stop func is idempotent;
// once it returns, send is never called again.
func pollSteer(t Turn, ready func() bool, send func(text string)) (stop func()) {
	t.SteerInput = nil // Text-only drivers leave images queued, preserving their ordering.
	return pollSteerInput(t, ready, func(text string, _ []message.Attachment) { send(text) })
}

func pollSteerInput(t Turn, ready func() bool, send func(string, []message.Attachment)) (stop func()) {
	if (t.Steer == nil && t.SteerInput == nil) || t.NoTools {
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
			case <-t.SteerReady:
			case <-tick.C:
			}
			mu.Lock()
			if !stopped && ready() {
				var text string
				var attachments []message.Attachment
				if t.SteerInput != nil {
					text, attachments = t.SteerInput()
				} else {
					text = t.Steer()
				}
				if text != "" || len(attachments) != 0 {
					send(text, attachments)
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

// withImagePaths saves the prompt's images as Crush's own copies and lists
// their paths, for CLIs that take no image input but can open image files.
// The original may be a temporary file (a screenshot tool's) that is gone by
// the time the CLI looks.
func withImagePaths(prompt string, attachments []message.Attachment) string {
	var sb strings.Builder
	for _, a := range attachments {
		if !a.IsImage() {
			continue
		}
		path, err := saveImage(a)
		if err != nil {
			slog.Error("Saving image attachment", "error", err)
			continue
		}
		fmt.Fprintf(&sb, "\n%s", path)
	}
	if sb.Len() == 0 {
		return prompt
	}
	return prompt + "\n\n<system_info>The user attached these images; open them with your file-reading tool:</system_info>" + sb.String()
}

// saveImage writes an image attachment to Crush's attachment folder, named
// by its content so a resent image reuses the file.
func saveImage(a message.Attachment) (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	dir = filepath.Join(dir, "crush", "attachments")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	sum := sha256.Sum256(a.Content)
	ext := filepath.Ext(a.FileName)
	if ext == "" {
		ext = "." + strings.TrimPrefix(a.MimeType, "image/")
	}
	path := filepath.Join(dir, hex.EncodeToString(sum[:16])+ext)
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	return path, os.WriteFile(path, a.Content, 0o600)
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

// takeIn removes and returns the steered texts content contains; a CLI may
// hand several queued messages to the model at once.
func (s *steered) takeIn(content string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var took, kept []string
	for _, t := range s.texts {
		if strings.Contains(content, t) {
			took = append(took, t)
		} else {
			kept = append(kept, t)
		}
	}
	s.texts = kept
	return took
}

func (s *steered) pending() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.texts)
}

// proc is a CLI child process speaking newline-delimited JSON on stdio.
type proc struct {
	fileReview *filechange.ProcessReview
	cmd        *exec.Cmd
	stdin      io.WriteCloser
	lines      *bufio.Scanner
	stderr     *tailBuffer
	mu         sync.Mutex
	closed     bool
	quit       chan struct{} // closed by finish; see readLines
}

func startProc(dir, name string, args ...string) (*proc, error) {
	return startProcEnv(dir, nil, name, args...)
}

// startProcEnv is startProc with extra environment variables.
func startProcEnv(dir string, env []string, name string, args ...string) (*proc, error) {
	return startProcCommand(exec.Command(name, args...), dir, env)
}

func startReviewProc(dir string, env []string, review bool, name string, args ...string) (*proc, error) {
	return startProcCommandReview(exec.Command(name, args...), dir, env, review)
}

func startProcCommand(cmd *exec.Cmd, dir string, env []string) (*proc, error) {
	return startProcCommandReview(cmd, dir, env, false)
}

func startProcCommandReview(cmd *exec.Cmd, dir string, env []string, review bool) (*proc, error) {
	cmd.Dir = dir
	if env != nil {
		cmd.Env = append(os.Environ(), env...)
	}
	ownGroup(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	p := &proc{cmd: cmd, stdin: stdin, stderr: &tailBuffer{max: 8 << 10}, quit: make(chan struct{})}
	cmd.Stderr = p.stderr
	p.lines = bufio.NewScanner(stdout)
	p.lines.Buffer(make([]byte, 0, 1<<20), 64<<20)
	if review {
		p.fileReview, err = filechange.StartProcess(cmd, dir, config.GlobalCacheDir(), filepath.Dir(config.GlobalConfigData()))
	} else {
		err = cmd.Start()
	}
	if err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, fmt.Errorf("starting %s: %w", cmd.Path, err)
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

// readLines delivers stdout lines on a channel, closed when output ends,
// for procs that outlive one turn. Once the proc is finished, lines nobody
// takes are dropped so the CLI never blocks writing on its way out.
func (p *proc) readLines() <-chan []byte {
	ch := make(chan []byte)
	go func() {
		defer close(ch)
		for p.lines.Scan() {
			select {
			case ch <- bytes.Clone(p.lines.Bytes()):
			case <-p.quit:
			}
		}
	}()
	return ch
}

// finish waits for the process, killing it if it lingers.
func (p *proc) finish() {
	p.mu.Lock()
	select {
	case <-p.quit:
	default:
		close(p.quit)
	}
	p.mu.Unlock()
	p.closeInput()
	timer := time.AfterFunc(5*time.Second, p.kill)
	defer timer.Stop()
	if err := p.fileReview.Wait(p.cmd); err != nil {
		slog.Debug("Agent CLI exited", "cmd", p.cmd.Path, "error", err, "stderr", p.stderr.String())
	}
}

// openCalls tracks the shell commands a turn is waiting on. Stopping a turn
// stops only those: ones the model or the user moved to the background keep
// running.
type openCalls struct {
	mu    sync.Mutex
	calls map[string]openCall
}

// openCall is a shell command and when the CLI reported it.
type openCall struct {
	command string
	words   []string // the command split as a shell would, if it could be
	at      time.Time
}

type openCallsKey struct{}

func (o *openCalls) track(emit func(Event) error) func(Event) error {
	return func(e Event) error {
		o.mu.Lock()
		switch e.Type {
		case EventToolCall:
			var in struct {
				Command string `json:"command"`
			}
			if _, ok := o.calls[e.ID]; !ok && json.Unmarshal([]byte(e.Input), &in) == nil && in.Command != "" {
				words, _ := shell.Fields(in.Command, func(string) string { return "" })
				o.calls[e.ID] = openCall{command: in.Command, words: words, at: time.Now()}
			}
		case EventToolResult:
			delete(o.calls, e.ID)
		}
		o.mu.Unlock()
		return emit(e)
	}
}

func (o *openCalls) list() []openCall {
	o.mu.Lock()
	defer o.mu.Unlock()
	return slices.Collect(maps.Values(o.calls))
}

// watchCancel calls interrupt once ctx is done, then gives the CLI a few
// seconds to wind down before killing it. The returned func stops watching.
func (p *proc) watchCancel(ctx context.Context, interrupt func()) func() {
	calls, _ := ctx.Value(openCallsKey{}).(*openCalls)
	if calls != nil {
		foregroundCalls.Store(p.cmd.Process.Pid, calls)
	}
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			// Stop the commands the turn is waiting on first: some CLIs
			// start them detached, where stopping the CLI doesn't reach
			// them. Older ones run in the background and stay.
			if calls, _ := ctx.Value(openCallsKey{}).(*openCalls); calls != nil {
				p.killCommands(calls.list())
			}
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
	return func() {
		close(done)
		if calls != nil {
			foregroundCalls.CompareAndDelete(p.cmd.Process.Pid, calls)
		}
	}
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

// Observe shell tool calls at the protocol boundary so mutations are attached
// to the action which made them, including tools that exit with an error.
func (p *proc) reviewEvents(emit func(Event) error) func(Event) error {
	return func(event Event) error {
		if p.fileReview != nil {
			if event.Type == EventToolCall && event.Name == tools.BashToolName {
				var input struct {
					Command string `json:"command"`
				}
				if json.Unmarshal([]byte(event.Input), &input) == nil {
					p.fileReview.Begin(event.ID, input.Command)
				}
			}
			if event.Type == EventToolResult {
				event.Metadata = filechange.WithReview(event.Metadata, p.fileReview.End(event.ID))
			}
		}
		return emit(event)
	}
}

// Package cliagent runs turns through installed agent CLIs (Claude Code,
// Codex, Grok, OpenCode, AGY) instead of an HTTP API. Each CLI
// keeps its own tools, login and settings; this package speaks its
// streaming protocol and turns what it does into [Event]s that Crush
// renders like any other turn.
package cliagent

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"charm.land/catwalk/v3/pkg/catwalk"
	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/accounts"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/history"
	"github.com/charmbracelet/crush/internal/jsonfile"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/permission"
)

// ErrResume means the CLI could not reopen the native session it was asked
// to resume (deleted, expired or from another machine).
var ErrResume = errors.New("native session could not be resumed")

// EventType identifies what an [Event] carries.
type EventType int

const (
	EventText          EventType = iota // Text
	EventReasoning                      // Text
	EventToolStart                      // ID, Name
	EventToolCall                       // ID, Name, Input
	EventToolResult                     // ID, Name, Output, Metadata, IsError
	EventUsage                          // Usage (per model request)
	EventSession                        // Session (native session ID)
	EventUserMessage                    // Text (a steered message the CLI took in)
	EventCompacting                     // Compacting (native context compaction status)
	EventActivity                       // Provider event received, without content; Activity names what the model is doing, if known, and Text is the newest line of thinking the CLI holds back.
	EventContextBudget                  // Native compaction boundary, not the full model window.
	EventTodos                          // Input (the CLI's plan as Crush todos tool input)
	EventMarker                         // Marker only: the CLI's own ID for what it said so far
)

// Marker is a CLI's own ID for a Crush message, recorded as replies arrive
// so a branch can fork the CLI's memory at its cut (engine-design.md 5.11).
// Any event can carry one; it is recorded once the event is handled.
type Marker struct {
	Native string // the tool's session or thread ID
	ID     string // the tool's message ID, turn ID or prompt index
	Kind   string // "message" | "turn" | "prompt_index" | "step"
	For    string // "assistant" (the reply message) | "tool" (the latest tool-result message)
}

// Marker kinds and targets.
const (
	MarkerMessage     = "message"
	MarkerTurn        = "turn"
	MarkerPromptIndex = "prompt_index"
	MarkerStep        = "step"

	MarkerForAssistant = "assistant"
	MarkerForTool      = "tool"
)

// marks reports whether a turn reports markers. Only a turn of a Crush
// chat that keeps its native session has messages to mark.
func (t Turn) marks() bool { return t.SessionID != "" && !t.NoTools }

// ActivityWriting says the model is writing a tool call it hasn't sent yet,
// such as a long command or patch.
const ActivityWriting = "writing"

// ActivityThinking says the model is thinking.
const ActivityThinking = "thinking"

// TextBreak is sent as text when a new text block starts. It separates two
// of the model's messages that land in one step and is dropped otherwise.
const TextBreak = "\n\n"

// Event is one normalized piece of a CLI turn. Tool names and inputs are
// already translated to Crush's own tools where one matches, so the UI can
// use its native renderers.
type Event struct {
	ContextLimit     int64
	ContextUsed      int64
	ContextEstimated bool
	Compacting       bool
	Type             EventType
	Text             string
	ID               string
	Name             string
	Input            string
	Output           string
	Metadata         string
	// Data and MimeType carry a picture a tool result returned (base64).
	Data     string
	MimeType string
	IsError  bool
	Usage    fantasy.Usage
	Session  string
	Activity string
	Marker   *Marker
}

// Turn is one user prompt sent to a CLI.
type Turn struct {
	SessionID string // Crush session, used for permission prompts
	Prompt    string
	// Continue shows a reply the CLI started on its own between turns
	// (see [OnUnprompted]) instead of sending Prompt.
	Continue bool
	Resume   string // native session to continue; empty starts a new one
	// NewSession names the new native session, for a CLI Crush may name
	// it for ([ChoosesSession]), so the link is saved before the CLI
	// starts: a turn stopped before the CLI's first reply still leaves a
	// copy a lock must find (owner, 2026-11-20).
	NewSession string
	// Fork, set on a branch's first turn, makes the turn fork the tool's
	// session at the cut instead of starting or resuming one (see
	// [CanFork]); the source session is never resumed in place. A failed
	// fork returns an error matching [ErrNoFork] before any event.
	Fork        *ForkPoint
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
	// CodexBackground reserves a follow-up for one terminal. Its callback receives
	// the final status and output, including after this turn has returned.
	CodexBackground func(session, command string, osPID int) func(status, output string)
	// OnCodexUsage records the latest estimated credits for a native Codex turn.
	OnCodexUsage func(CodexUsage)
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
	// StallRetry is options.stall_retry_seconds: how long the model's reply
	// may stop arriving before it is asked again on a fresh request. Zero
	// leaves each CLI's own, much longer, limit.
	StallRetry time.Duration
	// AutoBackground is options.auto_background_seconds: how long a
	// command may hold the turn before Crush moves it to the background.
	// Zero means the default.
	AutoBackground time.Duration
}

// Model is a CLI-backed model. It implements [fantasy.LanguageModel] for
// one-shot text requests; full agent turns go through [Model.Run].
type Model struct {
	Kind        catwalk.Type
	ID          string
	ServiceTier string
	// Ultracode turns on Claude Code's Ultracode.
	Ultracode bool
	Dir       string
	Perms     permission.Service
	Files     history.Service
	Links     *Links
	// Guarded runs a background sub-agent: nobody is there to approve its
	// tool calls, so Crush answers them itself, granting whatever its own
	// YOLO mode allows. The CLI's bypass modes stay off so every call
	// still comes to Crush.
	Guarded bool
	// ReadOnly runs a sub-agent where it can't change the project's files.
	ReadOnly bool
	// NoMCP starts the CLI without the user's MCP add-on servers: each
	// helper would start its own copies, and few use them (owner,
	// 2026-10-30). Sub-agents run this way unless spawned with --mcp.
	NoMCP bool
	// Env is added to the CLI process environment on every turn.
	Env []string
	// Account is the Claude Code or Codex sign-in the model runs on (an
	// options.accounts ID); empty is the tool's own login.
	Account string
	// ContextWindow, when set, is the size Crush gives this model instead of
	// the one its CLI reports: OpenCode Zen's free DeepSeek keeps its whole
	// window, and OpenCode stops compacting that chat by itself.
	ContextWindow int64
}

// KeepsWholeContext reports whether Crush gave this model its own size, which
// only happens for OpenCode Zen's free DeepSeek while the switch is on.
func (m *Model) KeepsWholeContext() bool { return m != nil && m.ContextWindow > 0 }

// NewProvider returns a [fantasy.Provider] whose models run through the
// agent CLI of the given kind.
func NewProvider(kind catwalk.Type, dir, dataDir string, perms permission.Service, files history.Service, serviceTier string, ultracode bool, account string, freeDeepSeekFullContext bool) fantasy.Provider {
	return &provider{kind: kind, dir: dir, perms: perms, files: files, serviceTier: serviceTier, ultracode: ultracode, account: account, freeDeepSeekFullContext: freeDeepSeekFullContext, links: &Links{path: filepath.Join(dataDir, "cli-sessions.json")}}
}

type provider struct {
	kind        catwalk.Type
	serviceTier string
	ultracode   bool
	account     string
	// freeDeepSeekFullContext is options.opencode_zen_free_deepseek_full_context:
	// the free DeepSeek runs on its whole window, not Zen's 200000.
	freeDeepSeekFullContext bool
	dir                     string
	perms                   permission.Service
	files                   history.Service
	links                   *Links
}

func (p *provider) Name() string { return string(p.kind) }

func (p *provider) LanguageModel(_ context.Context, modelID string) (fantasy.LanguageModel, error) {
	m := &Model{Kind: p.kind, ID: modelID, ServiceTier: p.serviceTier, Ultracode: p.ultracode, Account: p.account, Dir: p.dir, Perms: p.perms, Files: p.files, Links: p.links}
	if p.freeDeepSeekFullContext && p.kind == config.TypeOpenCodeZenCLI && modelID == config.OpenCodeZenFreeDeepSeekModel {
		m.ContextWindow = config.OpenCodeZenFreeDeepSeekWindow
	}
	return m, nil
}

// Run executes one turn, emitting events until the CLI finishes it.
func (m *Model) Run(ctx context.Context, t Turn) error {
	if t.Continue && m.Kind != config.TypeClaudeCode {
		return nil // the session moved on to another CLI
	}
	if m.Account != "" && config.HasAccounts(m.Kind) {
		// Bring the account's folder up to date with account 1's.
		if _, err := accounts.Prepare(m.Kind, m.Account); err != nil {
			return fmt.Errorf("preparing %s: %w", m.Account, err)
		}
	}
	calls := &openCalls{calls: map[string]openCall{}, after: t.AutoBackground}
	if t.Emit != nil {
		t.Emit = calls.track(t.Emit)
	}
	// A command the CLI waits on for the user's time (2 minutes unless
	// changed) is moved to the background, never holding the turn past
	// the cap Crush's own bash tool keeps.
	if t.SessionID != "" && CanBackground(m.Kind) {
		defer calls.autoBackground(t.SessionID)()
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
		t.Env = grokEnv(m, t.Env)
		return runACP(ctx, m, t, "grok", append(args, "stdio"), false)
	case config.TypeOpenCodeCLI, config.TypeOpenCodeZenCLI:
		// OpenCode's ACP sessions start no MCP servers of their own; Crush
		// passes none, so NoMCP needs nothing here.
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
	t := Turn{Prompt: flattenPrompt(call.Prompt), NoTools: true, Effort: "low"}
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
	runModel := m
	if fastID := fastAGYTitleModel(m); fastID != "" {
		cp := *m
		cp.ID = fastID
		runModel = &cp
	}
	if err := runModel.Run(ctx, t); err != nil {
		if runModel != m && ctx.Err() == nil {
			text.Reset()
			usage = fantasy.Usage{}
			if fallbackErr := m.Run(ctx, t); fallbackErr != nil {
				return nil, fallbackErr
			}
		} else {
			return nil, err
		}
	}
	return &fantasy.Response{
		Content:      fantasy.ResponseContent{fantasy.TextContent{Text: text.String()}},
		FinishReason: fantasy.FinishReasonStop,
		Usage:        usage,
	}, nil
}

// fastAGYTitleModel picks the low-effort variant of an AGY model for short
// one-shot title/summary calls so high/xhigh models don't spend 30s thinking.
func fastAGYTitleModel(m *Model) string {
	if m.Kind != config.TypeAGYCLI {
		return ""
	}
	for _, prefix := range []string{"gemini-4-neon-", "gemini-4-carbon-"} {
		if strings.HasPrefix(m.ID, prefix) && m.ID != prefix+"low" {
			return prefix + "low"
		}
	}
	return ""
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
	TreeRevision int64  `json:"tree_revision,omitempty"`
	Native       string `json:"native"`
	// Through is the last Crush message the native session has seen.
	// Anything after it happened elsewhere and is handed over on resume.
	Through string `json:"through"`
	// Tasks is set once the native session was told how to spawn
	// sub-agents.
	Tasks bool `json:"tasks,omitempty"`
	// TaskInstructionsHash refreshes changed guidance in existing conversations.
	TaskInstructionsHash string `json:"task_instructions_hash,omitempty"`
	// SharedInstructions records delivery without invalidating the session.
	SharedInstructions bool `json:"shared_instructions,omitempty"`
}

// Links persists [Link]s per Crush session and CLI, in one file every
// Crush in the project shares.
// ponytail: one small JSON file rewritten per turn; a DB table if it grows.
type Links struct {
	path string
}

// Get returns the link for a session and CLI, or the zero Link.
func (l *Links) Get(sessionID string, kind catwalk.Type) Link {
	return jsonfile.Read[map[string]Link](l.path)[sessionID+"/"+string(kind)]
}

// Set stores the link for a session and CLI.
func (l *Links) Set(sessionID string, kind catwalk.Type, link Link) error {
	return jsonfile.Update(l.path, func(links *map[string]Link) bool {
		if *links == nil {
			*links = map[string]Link{}
		}
		(*links)[sessionID+"/"+string(kind)] = link
		return true
	})
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
// session's CLI could be asked: Claude, Codex, OpenCode and AGY can.
func Background(sessionID string) bool {
	v, ok := backgrounders.Load(sessionID)
	if !ok || !v.(*backgrounder).fn() {
		return false
	}
	NoteBackground()
	return true
}

// lastBackground is when a CLI command was last moved to the background (Unix
// nanoseconds).
var lastBackground atomic.Int64

// NoteBackground records that a CLI command was explicitly backgrounded right
// now (by Ctrl+B or by the CLI itself), so it is listed immediately instead of
// waiting for the background age threshold.
func NoteBackground() {
	lastBackground.Store(time.Now().UnixNano())
}

// LastBackground reports when the user last moved a CLI's running commands
// to the background, or the zero time if never. Commands already running
// then are background work even before they reach the usual age.
func LastBackground() time.Time {
	if ns := lastBackground.Load(); ns != 0 {
		return time.Unix(0, ns)
	}
	return time.Time{}
}

// CanBackground reports whether a CLI's running commands can be moved to
// the background with [Background].
func CanBackground(kind catwalk.Type) bool {
	return kind == config.TypeClaudeCode || kind == config.TypeCodexCLI || kind == config.TypeOpenCodeCLI || kind == config.TypeOpenCodeZenCLI || kind == config.TypeAGYCLI
}

// SteersAnyTime reports whether a CLI folds a queued message into its
// running turn at any step (OpenCode's own server takes it, Codex's
// turn/steer cuts the reply it is writing short, AGY answers it once the
// step it is on ends), not only while a tool runs.
// ChoosesSession tells whether Crush names a new native session itself
// ([Turn.NewSession]) instead of learning the name from the CLI.
func ChoosesSession(kind catwalk.Type) bool {
	return kind == config.TypeClaudeCode
}

func SteersAnyTime(kind catwalk.Type) bool {
	return kind == config.TypeOpenCodeCLI || kind == config.TypeOpenCodeZenCLI || kind == config.TypeCodexCLI || kind == config.TypeAGYCLI
}

// unprompted holds the handlers [OnUnprompted] registered, one per
// coordinator (a server can run several workspaces).
var unprompted struct {
	sync.Mutex
	handlers []*func(sessionID string) bool
}

// OnUnprompted registers a handler for a Crush session whose kept CLI
// process started a reply on its own between turns (a background task it ran
// finished). The handler that owns the session returns true and runs a turn
// with [Turn.Continue] set to show it. The returned func unregisters it.
func OnUnprompted(handler func(sessionID string) bool) (remove func()) {
	h := &handler
	unprompted.Lock()
	unprompted.handlers = append(unprompted.handlers, h)
	unprompted.Unlock()
	return func() {
		unprompted.Lock()
		defer unprompted.Unlock()
		unprompted.handlers = slices.DeleteFunc(unprompted.handlers, func(x *func(string) bool) bool { return x == h })
	}
}

// notifyUnprompted hands sessionID to the first handler that owns it.
func notifyUnprompted(sessionID string) {
	unprompted.Lock()
	handlers := slices.Clone(unprompted.handlers)
	unprompted.Unlock()
	for _, h := range handlers {
		if (*h)(sessionID) {
			return
		}
	}
}

// helpersRunning holds the handlers [OnHelpersRunning] registered, one per
// coordinator.
var helpersRunning struct {
	sync.Mutex
	handlers []*func(sessionID string) (release func(), ok bool)
}

// OnHelpersRunning registers a handler for a Crush session whose CLI ended
// its turn with helpers (sub-agents the CLI started itself) still working.
// The handler that owns the session returns ok and a release func: until
// it is called, a headless run on the session waits, so the helpers'
// results are not lost. The returned func unregisters it.
func OnHelpersRunning(handler func(sessionID string) (release func(), ok bool)) (remove func()) {
	h := &handler
	helpersRunning.Lock()
	helpersRunning.handlers = append(helpersRunning.handlers, h)
	helpersRunning.Unlock()
	return func() {
		helpersRunning.Lock()
		defer helpersRunning.Unlock()
		helpersRunning.handlers = slices.DeleteFunc(helpersRunning.handlers, func(x *func(string) (func(), bool)) bool { return x == h })
	}
}

// holdForHelpers asks the handler that owns sessionID to keep its headless
// run open, and returns the func that lets it go.
func holdForHelpers(sessionID string) func() {
	helpersRunning.Lock()
	handlers := slices.Clone(helpersRunning.handlers)
	helpersRunning.Unlock()
	for _, h := range handlers {
		if release, ok := (*h)(sessionID); ok {
			return release
		}
	}
	return func() {}
}

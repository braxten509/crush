package agent

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"charm.land/fantasy"

	"github.com/charmbracelet/crush/internal/agent/cliagent"
	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/diff"
	"github.com/charmbracelet/crush/internal/filechange"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/session"
)

// cliSteps turns a CLI's event stream into fantasy steps. A new step (and
// so a new assistant message) starts whenever the model talks again after
// running tools, matching how native turns are split.
type cliSteps struct {
	call          SessionAgentCall
	onQueuedInput func([]SessionAgentCall)

	ctx          context.Context
	sc           fantasy.AgentStreamCall
	m            *cliagent.Model
	a            *sessionAgent
	sessionID    string
	onCompacting func(bool) error
	onActivity   func() error
	compacting   bool
	// File content before each pending edit, by tool call ID.
	edits map[string][2]string
	// Last full content seen per file, for CLIs that report an edit only
	// once it is done (so the snapshot above is already the new content).
	known    map[string]string
	inputs   map[string]string
	open     bool
	thinking bool
	content  fantasy.ResponseContent
	usage    fantasy.Usage
	tools    int
	steps    []fantasy.StepResult
	native   string

	// Queued prompts handed to the CLI mid-turn, until it takes them in.
	// steer runs on the driver's poller, the rest on its read loop.
	// When both locks are needed, take the session dispatch mutex first.
	steerMu sync.Mutex
	steered []cliSteered
	// Buffered so enqueueing under the dispatch lock never waits on the
	// driver's queue drain, which takes the same lock.
	steerReady chan struct{}

	// running holds the tool calls that have started but not returned.
	// Queued prompts go to the CLI only then, so it adds them to the
	// tool's result: the next tool boundary.
	runningMu sync.Mutex
	running   map[string]bool

	// todoMeta is what each to-do tool call changed, shown with its result.
	todoMeta map[string]string
}

// setToolRunning records a tool call starting or returning. A start wakes
// the steering poller so queued prompts reach the CLI while it runs.
func (s *cliSteps) setToolRunning(id string, running bool) {
	s.runningMu.Lock()
	if running {
		if s.running == nil {
			s.running = map[string]bool{}
		}
		s.running[id] = true
	} else {
		delete(s.running, id)
	}
	s.runningMu.Unlock()
	if running {
		select {
		case s.steerReady <- struct{}{}:
		default:
		}
	}
}

func (s *cliSteps) toolRunning() bool {
	s.runningMu.Lock()
	defer s.runningMu.Unlock()
	return len(s.running) > 0
}

// takePendingSteered transfers pending prompts to Interrupt atomically
// against CLI acknowledgements. Late acknowledgements cannot consume a
// prompt whose ownership has already moved to the replacement turn.
func (s *cliSteps) takePendingSteered() []SessionAgentCall {
	s.steerMu.Lock()
	defer s.steerMu.Unlock()
	var calls []SessionAgentCall
	for _, st := range s.steered {
		calls = append(calls, st.calls...)
	}
	s.steered = nil
	return calls
}

type cliSteered struct {
	text  string
	calls []SessionAgentCall
}

// pendingSteered lists the prompts handed to the CLI but not taken in yet.
func (s *cliSteps) pendingSteered() []string {
	s.steerMu.Lock()
	defer s.steerMu.Unlock()
	var prompts []string
	for _, st := range s.steered {
		for _, q := range st.calls {
			prompts = append(prompts, q.Prompt)
		}
	}
	return prompts
}

// midTurnNote leads every prompt folded into a running turn. Without it the
// model often answers the message and ends the turn, leaving its work
// unfinished. The chat shows only the user's own words.
const midTurnNote = `<crush_mid_turn_message>
The user sent the message below while you were working. Answer it, then keep
going with your current work in this same turn. Saying you will continue is
not enough: keep using your tools until the work is done. If the message
changes the plan, follow the new plan. Stop only if it asks you to stop or wait.
</crush_mid_turn_message>

`

// steer takes the queued prompts for the CLI to fold into the running turn,
// while one of its tools runs.
func (s *cliSteps) steer() string {
	text, _ := s.steerCalls(false)
	return text
}

func (s *cliSteps) steerWithImages() (string, []message.Attachment) {
	return s.steerCalls(true)
}

func (s *cliSteps) steerCalls(images bool) (string, []message.Attachment) {
	mu := s.a.sessionMu(s.sessionID)
	mu.Lock()
	var canceled []SessionAgentCall
	defer func() {
		mu.Unlock()
		s.a.publishCanceledQueueDrops(canceled)
	}()
	if s.ctx.Err() != nil || !s.toolRunning() {
		return "", nil
	}
	queued, _ := s.a.messageQueue.Get(s.sessionID)
	if len(queued) > 0 && !s.a.compatibleQueuedCalls(s.call, queued[0]) {
		return "", nil
	}
	fold, canceled := s.a.drainQueueForStepLocked(s.sessionID)
	// A text-only driver cannot accept any portion of an image batch.
	// Keep the complete drain together for the next native turn.
	if !images && slices.ContainsFunc(fold, func(q SessionAgentCall) bool {
		return slices.ContainsFunc(q.Attachments, message.Attachment.IsImage)
	}) {
		s.a.requeueFrontLocked(s.sessionID, fold)
		return "", nil
	}
	if len(fold) == 0 {
		return "", nil
	}
	texts := make([]string, len(fold))
	var attachments []message.Attachment
	for i, q := range fold {
		text, err := cliPromptWithAttachments(q.Prompt, q.Attachments)
		if err != nil {
			s.a.requeueFrontLocked(s.sessionID, fold)
			slog.Error("Preparing native CLI attachments", "error", err)
			return "", nil
		}
		texts[i] = text
		for _, attachment := range q.Attachments {
			if attachment.IsImage() {
				attachments = append(attachments, attachment)
			}
		}
	}
	text := strings.Join(texts, "\n\n")
	if strings.TrimSpace(text) == "" && len(attachments) > 0 {
		text = "Please review the attached images."
	}
	text = midTurnNote + text
	s.steerMu.Lock()
	s.steered = append(s.steered, cliSteered{text: text, calls: fold})
	s.steerMu.Unlock()
	return text, attachments
}

// takeSteered removes and returns the prompts sent as text.
func (s *cliSteps) takeSteered(text string) []SessionAgentCall {
	s.steerMu.Lock()
	defer s.steerMu.Unlock()
	for i, st := range s.steered {
		if st.text == text {
			s.steered = append(s.steered[:i], s.steered[i+1:]...)
			return st.calls
		}
	}
	return nil
}

// returnUnsteered puts prompts the CLI never took in back at the front of
// the queue so they run as the next turn. A plain cancel drops them, the
// same as it drops the rest of the queue.
func (s *cliSteps) returnUnsteered() {
	mu := s.a.sessionMu(s.sessionID)
	mu.Lock()
	calls := s.takePendingSteered()
	if len(calls) == 0 {
		mu.Unlock()
		return
	}
	if s.ctx.Err() != nil {
		mu.Unlock()
		s.a.publishCanceledQueueDrops(calls)
		return
	}
	s.a.requeueFrontLocked(s.sessionID, calls)
	mu.Unlock()
}

func (s *cliSteps) begin() error {
	if _, _, err := s.sc.PrepareStep(s.ctx, fantasy.PrepareStepFunctionOptions{StepNumber: len(s.steps)}); err != nil {
		return err
	}
	s.open, s.content, s.usage, s.tools = true, nil, fantasy.Usage{}, 0
	return nil
}

func (s *cliSteps) setCompacting(active bool) error {
	if s.compacting == active {
		return nil
	}
	s.compacting = active
	if s.onCompacting != nil {
		return s.onCompacting(active)
	}
	return nil
}

func (s *cliSteps) finish(reason fantasy.FinishReason) error {
	if !s.open {
		return nil
	}
	if err := s.endThinking(); err != nil {
		return err
	}
	s.open = false
	step := fantasy.StepResult{Response: fantasy.Response{Content: s.content, FinishReason: reason, Usage: s.usage}}
	s.steps = append(s.steps, step)
	return s.sc.OnStepFinish(step)
}

func (s *cliSteps) endThinking() error {
	if !s.thinking {
		return nil
	}
	s.thinking = false
	return s.sc.OnReasoningEnd("0", fantasy.ReasoningContent{})
}

// talk makes sure the model's next words land in a fresh step if tools ran.
func (s *cliSteps) talk() error {
	if s.open && s.tools == 0 {
		return nil
	}
	if err := s.finish(fantasy.FinishReasonToolCalls); err != nil {
		return err
	}
	return s.begin()
}

// handle drives the turn's callbacks from one event of the agent CLI.
func (s *cliSteps) handle(e cliagent.Event) error {
	// Resume normal status on activity even if a CLI omits its end event.
	if s.compacting && (e.Type == cliagent.EventText || e.Type == cliagent.EventReasoning || e.Type == cliagent.EventToolStart || e.Type == cliagent.EventToolCall || e.Type == cliagent.EventUserMessage) {
		if err := s.setCompacting(false); err != nil {
			return err
		}
	}
	switch e.Type {
	case cliagent.EventActivity:
		if s.onActivity != nil {
			return s.onActivity()
		}
		return nil
	case cliagent.EventCompacting:
		return s.handleCompacting(e)
	case cliagent.EventSession:
		s.native = e.Session
	case cliagent.EventUsage:
		s.usage = e.Usage
	case cliagent.EventContextBudget:
		return s.handleContextBudget(e)
	case cliagent.EventTodos:
		_, err := s.saveTodos(e.Input)
		return err
	case cliagent.EventUserMessage:
		return s.handleUserMessage(e)
	case cliagent.EventReasoning:
		return s.handleReasoning(e)
	case cliagent.EventText:
		return s.handleText(e)
	case cliagent.EventToolStart:
		return s.handleToolStart(e)
	case cliagent.EventToolCall:
		return s.handleToolCall(e)
	case cliagent.EventToolResult:
		return s.handleToolResult(e)
	}
	return nil
}

func (s *cliSteps) handleCompacting(e cliagent.Event) error {
	if e.Compacting {
		// A completed tool step may still be the current message.
		// Give compaction its own visible assistant status.
		if err := s.talk(); err != nil {
			return err
		}
	}
	return s.setCompacting(e.Compacting)
}

// handleContextBudget records the context budget the CLI reports.
func (s *cliSteps) handleContextBudget(e cliagent.Event) error {
	current, err := s.a.sessions.Get(s.ctx, s.sessionID)
	if err != nil {
		return err
	}
	budget := session.ContextBudget{
		Provider: string(s.m.Kind), Model: s.m.ID,
		Tokens: e.ContextLimit, Estimated: e.ContextEstimated,
	}
	if current.ContextBudget == budget {
		return nil
	}
	current.ContextBudget = budget
	_, err = s.a.sessions.Save(s.ctx, current)
	return err
}

// handleUserMessage takes in the prompts the CLI was steered with.
func (s *cliSteps) handleUserMessage(e cliagent.Event) error {
	calls := s.takeSteered(e.Text)
	if len(calls) == 0 {
		return nil
	}
	if s.onQueuedInput != nil {
		s.onQueuedInput(calls)
	}
	// The user's message goes between the model's steps, as it does
	// when a native turn folds in queued prompts.
	reason := fantasy.FinishReasonStop
	if s.tools > 0 {
		reason = fantasy.FinishReasonToolCalls
	}
	if err := s.finish(reason); err != nil {
		return err
	}
	for _, q := range calls {
		if _, err := s.a.createUserMessage(s.ctx, q); err != nil {
			return err
		}
	}
	return s.begin()
}

func (s *cliSteps) handleReasoning(e cliagent.Event) error {
	if err := s.talk(); err != nil {
		return err
	}
	if !s.thinking {
		s.thinking = true
		return s.sc.OnReasoningStart("0", fantasy.ReasoningContent{Text: e.Text})
	}
	return s.sc.OnReasoningDelta("0", e.Text)
}

func (s *cliSteps) handleText(e cliagent.Event) error {
	if e.Text == cliagent.TextBreak && !s.hasText() {
		return nil
	}
	if err := s.talk(); err != nil {
		return err
	}
	if err := s.endThinking(); err != nil {
		return err
	}
	s.content = appendText(s.content, e.Text)
	return s.sc.OnTextDelta("0", e.Text)
}

func (s *cliSteps) handleToolStart(e cliagent.Event) error {
	if err := s.endThinking(); err != nil {
		return err
	}
	s.tools++
	s.setToolRunning(e.ID, true)
	return s.sc.OnToolInputStart(e.ID, e.Name)
}

// handleToolCall records a tool call, keeping the file an edit is about to
// change so its result can carry a whole-file diff.
func (s *cliSteps) handleToolCall(e cliagent.Event) error {
	s.tools++
	s.setToolRunning(e.ID, true)
	if path := cliagent.EditedFile(e.Name, e.Input); path != "" {
		if !filepath.IsAbs(path) {
			path = filepath.Join(s.m.Dir, path)
		}
		before, _ := os.ReadFile(path)
		if s.edits == nil {
			s.edits = map[string][2]string{}
		}
		s.edits[e.ID] = [2]string{path, string(before)}
	}
	if s.inputs == nil {
		s.inputs = map[string]string{}
	}
	s.inputs[e.ID] = e.Input
	if e.Name == tools.TodosToolName {
		meta, err := s.saveTodos(e.Input)
		if err != nil {
			return err
		}
		if meta != "" {
			if s.todoMeta == nil {
				s.todoMeta = map[string]string{}
			}
			s.todoMeta[e.ID] = meta
		}
	}
	tc := fantasy.ToolCallContent{ToolCallID: e.ID, ToolName: e.Name, Input: e.Input}
	s.content = append(s.content, tc)
	return s.sc.OnToolCall(tc)
}

// handleToolResult records a tool result, with the whole-file diff of an
// edit and the file contents a view showed.
func (s *cliSteps) handleToolResult(e cliagent.Event) error {
	metadata, reportedReview := filechange.TakeReview(e.Metadata)
	e.Metadata = metadata
	s.setToolRunning(e.ID, false)
	var out fantasy.ToolResultOutputContent = fantasy.ToolResultOutputContentText{Text: e.Output}
	if e.IsError {
		out = fantasy.ToolResultOutputContentError{Error: errors.New(e.Output)}
	}
	if s.known == nil {
		s.known = map[string]string{}
	}
	if edit, ok := s.edits[e.ID]; ok && !e.IsError {
		e.Metadata = s.editMetadata(e, edit)
	}
	if meta, ok := s.todoMeta[e.ID]; ok && !e.IsError {
		e.Metadata = meta
	}
	if e.Name == tools.ViewToolName && !e.IsError && !strings.Contains(s.inputs[e.ID], `"offset"`) && !strings.Contains(s.inputs[e.ID], `"limit"`) {
		var view tools.ViewResponseMetadata
		if json.Unmarshal([]byte(e.Metadata), &view) == nil && view.FilePath != "" {
			s.known[view.FilePath] = view.Content
		}
	}
	e.Metadata = filechange.WithReview(e.Metadata, reportedReview)
	tr := fantasy.ToolResultContent{ToolCallID: e.ID, ToolName: e.Name, Result: out, ClientMetadata: e.Metadata}
	s.content = append(s.content, tr)
	return s.sc.OnToolResult(tr)
}

// editMetadata records a successful edit of edit's file (its path and its
// contents before the call) and returns the result's metadata.
func (s *cliSteps) editMetadata(e cliagent.Event, edit [2]string) string {
	after, _ := os.ReadFile(edit[0])
	if edit[1] == string(after) {
		// The CLI wrote the file before Crush read its "before" copy.
		var reported tools.EditResponseMetadata
		_ = json.Unmarshal([]byte(e.Metadata), &reported)
		switch old, known := s.known[edit[0]]; {
		case reported.Additions+reported.Removals > 0 && reported.NewContent == string(after):
			edit[1] = reported.OldContent // a full-file diff from the CLI itself
		case known:
			edit[1] = old
		case e.Name == tools.WriteToolName && e.Metadata == "":
			// Writing over an existing file needs a read first, which
			// would be known, so this file was new.
			edit[1] = ""
		}
	}
	s.known[edit[0]] = string(after)
	s.m.RecordEdit(s.ctx, s.sessionID, edit[0], edit[1])
	// Diff the whole file, as Crush's own edit tool does, rather
	// than the replaced text the CLI reports: the diff then has
	// real line numbers.
	// A late event can arrive with the new contents already on disk.
	// Keep the CLI's reported diff instead of replacing it with an
	// empty local diff when no earlier full-file content is known.
	if edit[1] != string(after) || e.Metadata == "" {
		_, adds, dels := diff.GenerateDiff(edit[1], string(after), edit[0])
		meta, _ := json.Marshal(tools.EditResponseMetadata{Additions: adds, Removals: dels, OldContent: edit[1], NewContent: string(after)})
		return string(meta)
	}
	return e.Metadata
}

func (s *cliSteps) result() *fantasy.AgentResult {
	res := &fantasy.AgentResult{Steps: s.steps}
	for _, step := range s.steps {
		res.TotalUsage.InputTokens += step.Usage.InputTokens
		res.TotalUsage.OutputTokens += step.Usage.OutputTokens
		res.TotalUsage.TotalTokens += step.Usage.TotalTokens
		if step.Content.Text() != "" {
			res.Response = step.Response
		}
	}
	return res
}

// hasText reports whether the open step ends in text.
func (s *cliSteps) hasText() bool {
	if !s.open || s.tools > 0 || len(s.content) == 0 {
		return false
	}
	_, ok := s.content[len(s.content)-1].(fantasy.TextContent)
	return ok
}

func appendText(content fantasy.ResponseContent, delta string) fantasy.ResponseContent {
	if n := len(content); n > 0 {
		if t, ok := content[n-1].(fantasy.TextContent); ok {
			t.Text += delta
			content[n-1] = t
			return content
		}
	}
	return append(content, fantasy.TextContent{Text: delta})
}

// saveTodos makes the CLI's own to-do list the chat's, the one the todos
// tool keeps for API models, and returns what changed for the chat to show.
// A list Crush can't read is left alone.
func (s *cliSteps) saveTodos(input string) (string, error) {
	var params tools.TodosParams
	if json.Unmarshal([]byte(input), &params) != nil {
		return "", nil
	}
	current, err := s.a.sessions.Get(s.ctx, s.sessionID)
	if err != nil {
		return "", err
	}
	todos, meta, err := tools.UpdateTodos(current.Todos, params.Todos)
	if err != nil {
		slog.Debug("Ignoring a CLI to-do list Crush can't read", "error", err)
		return "", nil
	}
	if !slices.Equal(current.Todos, todos) {
		current.Todos = todos
		if _, err := s.a.sessions.Save(s.ctx, current); err != nil {
			return "", err
		}
	}
	data, _ := json.Marshal(meta)
	return string(data), nil
}

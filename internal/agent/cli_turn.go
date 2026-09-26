package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/agent/cliagent"
	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/message"
)

// cliStream stands in for fantasy's Agent.Stream when the model is an agent
// CLI. The CLI runs its own tool loop; this drives the same callbacks a
// native turn would (one step per model response), so messages, usage,
// permissions and rendering work exactly as they do for API models.
func (a *sessionAgent) cliStream(m *cliagent.Model, call SessionAgentCall, history []message.Message, effort string) func(context.Context, fantasy.AgentStreamCall) (*fantasy.AgentResult, error) {
	return func(ctx context.Context, sc fantasy.AgentStreamCall) (*fantasy.AgentResult, error) {
		link := m.Links.Get(call.SessionID, m.Kind)
		prompt := message.PromptWithTextAttachments(call.Prompt, call.Attachments)
		text, resume := cliHandoff(history, link, prompt)

		s := &cliSteps{ctx: ctx, sc: sc, m: m, a: a, sessionID: call.SessionID}
		if err := s.begin(); err != nil {
			return nil, err
		}
		defer s.returnUnsteered()
		turn := cliagent.Turn{SessionID: call.SessionID, Prompt: text, Resume: resume, Effort: effort, Emit: s.handle, Steer: s.steer}
		err := m.Run(ctx, turn)
		if errors.Is(err, cliagent.ErrResume) {
			// The native session is gone; hand the whole conversation to a
			// fresh one instead.
			slog.Warn("Agent CLI could not resume its session; starting fresh", "cli", m.Kind, "session", resume)
			turn.Prompt, turn.Resume = cliHandoff(history, cliagent.Link{}, prompt)
			err = m.Run(ctx, turn)
		}

		// Whatever happened, the native session has now seen everything up
		// to here; the next turn resumes it and only hands over what other
		// agents add in between.
		if s.native != "" {
			a.saveCLILink(context.WithoutCancel(ctx), m, call.SessionID, s.native)
		}
		if err != nil {
			return nil, err
		}
		if err := s.finish(fantasy.FinishReasonStop); err != nil {
			return nil, err
		}
		// The CLI can't be stopped between its own steps, so the stop
		// conditions (auto-summarize among them) are checked once the turn
		// is done.
		for _, stop := range sc.StopWhen {
			stop(s.steps)
		}
		return s.result(), nil
	}
}

func (a *sessionAgent) saveCLILink(ctx context.Context, m *cliagent.Model, sessionID, native string) {
	msgs, err := a.messages.List(ctx, sessionID)
	if err != nil || len(msgs) == 0 {
		return
	}
	link := cliagent.Link{Native: native, Through: msgs[len(msgs)-1].ID}
	if err := m.Links.Set(sessionID, m.Kind, link); err != nil {
		slog.Error("Failed to save agent CLI session link", "error", err)
	}
}

// cliSteps turns a CLI's event stream into fantasy steps. A new step (and
// so a new assistant message) starts whenever the model talks again after
// running tools, matching how native turns are split.
type cliSteps struct {
	ctx       context.Context
	sc        fantasy.AgentStreamCall
	m         *cliagent.Model
	a         *sessionAgent
	sessionID string
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
	steerMu sync.Mutex
	steered []cliSteered
}

type cliSteered struct {
	text  string
	calls []SessionAgentCall
}

// steer takes the queued prompts for the CLI to fold into the running turn.
func (s *cliSteps) steer() string {
	fold, canceled := s.a.drainQueueForStep(s.sessionID)
	s.a.publishCanceledQueueDrops(canceled)
	if len(fold) == 0 {
		return ""
	}
	texts := make([]string, len(fold))
	for i, q := range fold {
		texts[i] = message.PromptWithTextAttachments(q.Prompt, q.Attachments)
	}
	text := strings.Join(texts, "\n\n")
	s.steerMu.Lock()
	s.steered = append(s.steered, cliSteered{text: text, calls: fold})
	s.steerMu.Unlock()
	return text
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
	s.steerMu.Lock()
	var calls []SessionAgentCall
	for _, st := range s.steered {
		calls = append(calls, st.calls...)
	}
	s.steered = nil
	s.steerMu.Unlock()
	if len(calls) == 0 {
		return
	}
	if interrupted, _ := s.a.interrupted.Get(s.sessionID); s.ctx.Err() != nil && !interrupted {
		return
	}
	s.a.requeueFront(s.sessionID, calls)
}

func (s *cliSteps) begin() error {
	if _, _, err := s.sc.PrepareStep(s.ctx, fantasy.PrepareStepFunctionOptions{StepNumber: len(s.steps)}); err != nil {
		return err
	}
	s.open, s.content, s.usage, s.tools = true, nil, fantasy.Usage{}, 0
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

func (s *cliSteps) handle(e cliagent.Event) error {
	switch e.Type {
	case cliagent.EventSession:
		s.native = e.Session
	case cliagent.EventUsage:
		s.usage = e.Usage
	case cliagent.EventUserMessage:
		calls := s.takeSteered(e.Text)
		if len(calls) == 0 {
			return nil
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
	case cliagent.EventReasoning:
		if err := s.talk(); err != nil {
			return err
		}
		if !s.thinking {
			s.thinking = true
			return s.sc.OnReasoningStart("0", fantasy.ReasoningContent{Text: e.Text})
		}
		return s.sc.OnReasoningDelta("0", e.Text)
	case cliagent.EventText:
		if err := s.talk(); err != nil {
			return err
		}
		if err := s.endThinking(); err != nil {
			return err
		}
		s.content = appendText(s.content, e.Text)
		return s.sc.OnTextDelta("0", e.Text)
	case cliagent.EventToolStart:
		if err := s.endThinking(); err != nil {
			return err
		}
		s.tools++
		return s.sc.OnToolInputStart(e.ID, e.Name)
	case cliagent.EventToolCall:
		s.tools++
		if path := cliagent.EditedFile(e.Name, e.Input); path != "" {
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
		tc := fantasy.ToolCallContent{ToolCallID: e.ID, ToolName: e.Name, Input: e.Input}
		s.content = append(s.content, tc)
		return s.sc.OnToolCall(tc)
	case cliagent.EventToolResult:
		var out fantasy.ToolResultOutputContent = fantasy.ToolResultOutputContentText{Text: e.Output}
		if e.IsError {
			out = fantasy.ToolResultOutputContentError{Error: errors.New(e.Output)}
		}
		if s.known == nil {
			s.known = map[string]string{}
		}
		if edit, ok := s.edits[e.ID]; ok && !e.IsError {
			after, _ := os.ReadFile(edit[0])
			if old, ok := s.known[edit[0]]; ok && edit[1] == string(after) {
				edit[1] = old
			}
			s.known[edit[0]] = string(after)
			s.m.RecordEdit(s.ctx, s.sessionID, edit[0], edit[1])
			if e.Metadata == "" {
				// The CLI didn't say what changed; diff the file itself.
				meta, _ := json.Marshal(tools.EditResponseMetadata{OldContent: edit[1], NewContent: string(after)})
				e.Metadata = string(meta)
			}
		}
		if e.Name == tools.ViewToolName && !e.IsError && !strings.Contains(s.inputs[e.ID], `"offset"`) && !strings.Contains(s.inputs[e.ID], `"limit"`) {
			var view tools.ViewResponseMetadata
			if json.Unmarshal([]byte(e.Metadata), &view) == nil && view.FilePath != "" {
				s.known[view.FilePath] = view.Content
			}
		}
		tr := fantasy.ToolResultContent{ToolCallID: e.ID, ToolName: e.Name, Result: out, ClientMetadata: e.Metadata}
		s.content = append(s.content, tr)
		return s.sc.OnToolResult(tr)
	}
	return nil
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

// Handoff budgets, in bytes. The newest messages are kept whole first.
const (
	handoffBudget     = 120_000
	handoffUserMax    = 12_000
	handoffTextMax    = 6_000
	handoffToolArgMax = 300
	handoffToolOutMax = 800
)

// cliHandoff builds the prompt for a CLI turn and picks the native session
// to resume. A native session that has seen the conversation up to some
// point is resumed with only what happened since (turns by other agents);
// otherwise a fresh session gets the whole conversation.
func cliHandoff(history []message.Message, link cliagent.Link, prompt string) (string, string) {
	if link.Native != "" {
		for i, msg := range history {
			if msg.ID != link.Through {
				continue
			}
			missed := history[i+1:]
			if len(missed) == 0 {
				return prompt, link.Native
			}
			return handoffPrompt("Since your last reply, the user continued this conversation with other AI agents. Here is what happened meanwhile:", missed, prompt), link.Native
		}
	}
	if len(history) == 0 {
		return prompt, ""
	}
	return handoffPrompt("This conversation started before you joined; earlier turns were handled by other AI agents. Here it is so far:", history, prompt), ""
}

func handoffPrompt(intro string, msgs []message.Message, prompt string) string {
	return fmt.Sprintf(`<conversation_handoff>
You are running inside Crush, a terminal app where the user can switch between AI agents mid-conversation. %s

%s
Treat this as your own context: build on finished work instead of redoing it, and don't mention the handoff unless asked.
</conversation_handoff>

%s`, intro, transcript(msgs), prompt)
}

// transcript renders messages for a handoff, newest first within the
// budget, then restores reading order.
func transcript(msgs []message.Message) string {
	var blocks []string
	size, omitted := 0, 0
	for i := len(msgs) - 1; i >= 0; i-- {
		block := renderMessage(msgs[i])
		if block == "" {
			continue
		}
		if size+len(block) > handoffBudget {
			omitted = i + 1
			break
		}
		size += len(block)
		blocks = append(blocks, block)
	}
	var b strings.Builder
	if omitted > 0 {
		fmt.Fprintf(&b, "[%d earlier messages omitted]\n\n", omitted)
	}
	for i := len(blocks) - 1; i >= 0; i-- {
		b.WriteString(blocks[i])
		b.WriteString("\n")
	}
	return b.String()
}

func renderMessage(msg message.Message) string {
	var b strings.Builder
	switch msg.Role {
	case message.User:
		if text := msg.Content().Text; text != "" {
			fmt.Fprintf(&b, "[user]\n%s\n", clip(text, handoffUserMax))
		}
	case message.Assistant:
		who := "assistant"
		if msg.Model != "" {
			who = fmt.Sprintf("assistant: %s/%s", msg.Provider, msg.Model)
		}
		if msg.IsSummaryMessage {
			who = "summary of the earlier conversation"
		}
		text := msg.Content().Text
		calls := msg.ToolCalls()
		if text == "" && len(calls) == 0 {
			return ""
		}
		fmt.Fprintf(&b, "[%s]\n", who)
		if text != "" {
			b.WriteString(clip(text, handoffTextMax) + "\n")
		}
		for _, tc := range calls {
			fmt.Fprintf(&b, "-> %s %s\n", tc.Name, clip(tc.Input, handoffToolArgMax))
		}
	case message.Tool:
		for _, tr := range msg.ToolResults() {
			status := "ok"
			if tr.IsError {
				status = "error"
			}
			fmt.Fprintf(&b, "<- %s %s: %s\n", tr.Name, status, clip(strings.TrimSpace(tr.Content), handoffToolOutMax))
		}
	}
	return b.String()
}

// clip shortens s to about max bytes, keeping its start and end.
func clip(s string, max int) string {
	if len(s) <= max {
		return s
	}
	half := max / 2
	return strings.ToValidUTF8(s[:half], "") + "\n[... middle omitted ...]\n" + strings.ToValidUTF8(s[len(s)-half:], "")
}

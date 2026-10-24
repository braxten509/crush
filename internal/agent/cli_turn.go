package agent

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/agent/cliagent"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/db"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/session"
)

// cliPromptWithAttachments carries non-image binary files through native
// drivers that only accept text and images. Text files remain inline; the
// cache references hold the original bytes even if the source disappears.
func cliPromptWithAttachments(prompt string, attachments []message.Attachment) (string, error) {
	text := message.PromptWithTextAttachments(prompt, attachments)
	for _, attachment := range attachments {
		if attachment.IsText() || attachment.IsImage() {
			continue
		}
		cache, err := os.UserCacheDir()
		if err != nil {
			return "", fmt.Errorf("attachment cache: %w", err)
		}
		directory := filepath.Join(cache, "crush", "attachments")
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return "", fmt.Errorf("attachment cache: %w", err)
		}
		extension := filepath.Ext(attachment.FileName)
		if extension == "" {
			extension = filepath.Ext(attachment.FilePath)
		}
		if extension == "" {
			extension = ".bin"
		}
		path := filepath.Join(directory, fmt.Sprintf("%x%s", sha256.Sum256(attachment.Content), extension))
		if err := os.WriteFile(path, attachment.Content, 0o600); err != nil {
			return "", fmt.Errorf("saving attachment: %w", err)
		}
		text += fmt.Sprintf("\n\nAttached file %q (%s), available at %s", attachment.FileName, attachment.MimeType, path)
	}
	return text, nil
}

// cliStream stands in for fantasy's Agent.Stream when the model is an agent
// CLI. The CLI runs its own tool loop; this drives the same callbacks a
// native turn would (one step per model response), so messages, usage,
// permissions and rendering work exactly as they do for API models.
// lastToolMessage reports the newest tool message the turn saved, for the
// CLI's tool markers.
func (a *sessionAgent) cliStream(m *cliagent.Model, call SessionAgentCall, history []message.Message, effort string, onCompacting func(bool) error, onActivity func(activity, detail string) error, lastToolMessage func() string, interruptToolCall func() bool) func(context.Context, fantasy.AgentStreamCall) (*fantasy.AgentResult, error) {
	showCompacting := onCompacting
	onCompacting = func(active bool) error {
		if active {
			// The CLI is about to forget details: resend kept skills in full.
			forgetKeptSkillsSent(call.SessionID)
		}
		if showCompacting == nil {
			return nil
		}
		return showCompacting(active)
	}
	return func(ctx context.Context, sc fantasy.AgentStreamCall) (*fantasy.AgentResult, error) {
		link, err := a.treeCLILink(ctx, m, call.SessionID)
		if err != nil {
			return nil, err
		}
		// A branch's first turn with this tool forks the tool's own memory
		// at the cut; the handoff then adds only what came after it.
		var fork *cliFork
		if link.Native == "" {
			if f, ok := a.branchFork(ctx, m, call.SessionID, history); ok {
				fork, link = &f, f.link
			}
		}
		prompt, err := cliPromptWithAttachments(call.Prompt, call.Attachments)
		if err != nil {
			return nil, err
		}
		text, resume := cliHandoffWithTranscript(history, link, prompt, a.transcriptDataDir(), call.SessionID)
		if fork != nil && len(fork.note) > 0 {
			text = handoffPromptWithTranscript(branchNoteIntro, fork.note, history, text, a.transcriptDataDir(), call.SessionID)
		}
		if call.todoHandover != "" {
			text = call.todoHandover + "\n\n" + text
		}
		// Refresh even resumed CLI sessions: their native skill catalogs and
		// kept system prompts may predate a /skills toggle or installation.
		skillPolicy := currentNativeSkillPolicy(a.cfg)
		if skillPolicy != "" {
			text = skillPolicy + "\n\n" + text
		}
		if note := memoryStatus().AgentNote(); note != "" {
			text = note + "\n\n" + text
		}
		turnedOff := func() []session.KeptSkill { return a.sessionTurnedOffSkills(ctx, call.SessionID) }
		if kept := cliKeptSkillsText(call.SessionID, string(m.Kind), a.sessionKeptSkills(ctx, call.SessionID, true), turnedOff, resume == ""); kept != "" {
			text = kept + "\n\n" + text
		}
		env := slices.Clone(m.Env)
		var instructions, taskInstructionsHash string
		if a.tasks != nil {
			taskInstructions := a.tasks.instructions()
			taskInstructionsHash = fmt.Sprintf("%x", sha256.Sum256([]byte(taskInstructions)))
			env = append(env, a.tasks.env(call.SessionID)...)
			switch {
			case m.Kind == config.TypeClaudeCode:
				// Claude takes them as part of its system prompt, which
				// survives its own compaction.
				instructions = taskInstructions
			case resume == "" || !link.Tasks || link.TaskInstructionsHash != taskInstructionsHash:
				text = taskInstructions + "\n\n" + text
			}
		}
		// Native memory is disabled; every CLI receives Crush's shared store.
		memory := sharedCLIInstructions + "\n\n" + memoryInstructions(a.isSubAgent)
		if m.Kind == config.TypeClaudeCode && memory != "" {
			instructions = memory + "\n\n" + instructions
		} else if memory != "" && (resume == "" || !link.SharedInstructions) {
			text = memory + "\n\n" + text
		}

		s := &cliSteps{call: call, onQueuedInput: call.onQueuedInput, ctx: ctx, sc: sc, m: m, a: a, sessionID: call.SessionID, onCompacting: onCompacting, onActivity: onActivity, onToolInterrupt: interruptToolCall, lastToolMessage: lastToolMessage, steerReady: make(chan struct{}, 1)}
		if err := s.begin(); err != nil {
			return nil, err
		}
		a.steering.Set(call.SessionID, s)
		defer a.steering.CompareAndDelete(call.SessionID, s)
		defer s.returnUnsteered()
		// Prompts sent mid-turn go in at the next tool boundary (see
		// steerCalls), or wait for the turn to end if no tool runs.
		turn := cliagent.Turn{SessionID: call.SessionID, Prompt: text, Continue: call.CLIContinue, Attachments: call.Attachments, Resume: resume, Effort: effort, Emit: s.handle, Steer: s.steer, SteerInput: s.steerWithImages, SteerReady: s.steerReady, Env: env, Instructions: instructions}
		if fork != nil {
			// The fork starts from the source's session; that session itself
			// is never resumed in place.
			turn.Resume, turn.Fork = "", &fork.point
		}
		err = m.Run(ctx, turn)
		if errors.Is(err, cliagent.ErrResume) || (turn.Fork != nil && errors.Is(err, cliagent.ErrNoFork)) {
			// The native session is gone, or the fork failed; hand the
			// whole conversation to a fresh one instead.
			if turn.Fork != nil {
				slog.Warn("Agent CLI could not fork its session; handing over a written copy", "cli", m.Kind, "session", turn.Fork.Native, "error", err)
			} else {
				slog.Warn("Agent CLI could not resume its session; starting fresh", "cli", m.Kind, "session", resume)
			}
			turn.Fork = nil
			turn.Prompt, turn.Resume = cliHandoffWithTranscript(history, cliagent.Link{}, prompt, a.transcriptDataDir(), call.SessionID)
			if skillPolicy != "" {
				turn.Prompt = skillPolicy + "\n\n" + turn.Prompt
			}
			if a.tasks != nil && instructions == "" {
				turn.Prompt = a.tasks.instructions() + "\n\n" + turn.Prompt
			}
			if memory != "" {
				turn.Prompt = memory + "\n\n" + turn.Prompt
			}
			err = m.Run(ctx, turn)
		}

		// Whatever happened, the native session has now seen everything up
		// to here; the next turn resumes it and only hands over what other
		// agents add in between.
		if s.native != "" {
			a.saveCLILink(context.WithoutCancel(ctx), m, call.SessionID, s.native, a.tasks != nil, taskInstructionsHash)
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

// cliSummarize writes the session summary from inside the CLI's own session,
// which already holds the conversation (cached), instead of pasting the
// whole transcript into a fresh request. It returns nil, nil when the CLI
// has no session to summarize from.
func (a *sessionAgent) cliSummarize(ctx context.Context, m *cliagent.Model, sessionID, effort string, history []message.Message, prompt string, summary *message.Message) (*fantasy.AgentResult, error) {
	link, err := a.treeCLILink(ctx, m, sessionID)
	if err != nil {
		return nil, err
	}
	if link.Native == "" {
		return nil, nil
	}
	prompt = string(summaryPrompt) + "\n\n" + prompt + "\n\nDon't use any tools: reply with the summary only."
	text, resume := cliHandoffWithTranscript(history, link, prompt, a.transcriptDataDir(), sessionID)
	if resume == "" {
		return nil, nil
	}
	var total, last fantasy.Usage
	err = m.Run(ctx, cliagent.Turn{SessionID: sessionID, Prompt: text, Resume: resume, Effort: effort, Emit: func(e cliagent.Event) error {
		switch e.Type {
		case cliagent.EventText:
			summary.AppendContent(e.Text)
		case cliagent.EventToolCall:
			// Anything said before a tool call isn't the summary.
			summary.ResetStreamedContent()
		case cliagent.EventUsage:
			last = e.Usage
			total.InputTokens += e.Usage.InputTokens
			total.OutputTokens += e.Usage.OutputTokens
			total.TotalTokens += e.Usage.TotalTokens
			total.CacheCreationTokens += e.Usage.CacheCreationTokens
			total.CacheReadTokens += e.Usage.CacheReadTokens
		default:
			return nil
		}
		return a.messages.Update(ctx, *summary)
	}})
	if err != nil {
		return nil, err
	}
	return &fantasy.AgentResult{TotalUsage: total, Response: fantasy.Response{Usage: last}}, nil
}

func (a *sessionAgent) saveCLILink(ctx context.Context, m *cliagent.Model, sessionID, native string, tasks bool, taskInstructionsHash string) {
	msgs, err := a.messages.List(ctx, sessionID)
	if err != nil || len(msgs) == 0 {
		return
	}
	link := cliagent.Link{Native: native, Through: msgs[len(msgs)-1].ID, Tasks: tasks, TaskInstructionsHash: taskInstructionsHash, SharedInstructions: true}
	if tree, ok := a.messages.(message.TreeService); ok {
		revision, err := tree.TreeRevision(ctx, sessionID)
		if err != nil {
			slog.Error("Failed to read tree revision", "error", err)
			return
		}
		link.TreeRevision = revision
	}
	if err := m.Links.Set(sessionID, m.Kind, link); err != nil {
		slog.Error("Failed to save agent CLI session link", "error", err)
	}
}

// branchNoteIntro introduces the part of a cut turn a forked branch keeps
// (desktop-design 3.4).
const branchNoteIntro = "This chat is a branch made partway through your last turn. Here is the part of that turn the branch keeps:"

// cliFork is how a branch's first turn with an agent CLI forks the CLI's
// own memory.
type cliFork struct {
	point cliagent.ForkPoint
	// link stands in for the fork in the handoff: the source session,
	// having seen the branch through the last copied message.
	link cliagent.Link
	// note is the rest of a turn the cut fell inside: the copied messages
	// after the fork's marker.
	note []message.Message
}

// branchFork finds where a branch's first turn with m forks m's memory
// (desktop-design 3.4): at m's marker on the last message the branch copied
// or, without one there, on the last earlier message that has one, the
// messages in between going in as the partial-turn note. The marker names
// the native session it was recorded in, so a branch of a branch forks its
// parent's own session. ok is false, and the written copy runs, when the
// chat is not a branch, its cut is off the path handed over, no copied
// message has a marker of m, or m can't fork there.
func (a *sessionAgent) branchFork(ctx context.Context, m *cliagent.Model, sessionID string, history []message.Message) (cliFork, bool) {
	origins, ok := a.messages.(message.BranchOrigins)
	if !ok {
		return cliFork{}, false
	}
	readers, ok := a.messages.(message.MarkerReader)
	if !ok {
		return cliFork{}, false
	}
	origin, isBranch, err := origins.BranchOrigin(ctx, sessionID)
	if err != nil || !isBranch {
		return cliFork{}, false
	}
	path, last, ok := a.branchCut(ctx, sessionID, origin)
	if !ok {
		return cliFork{}, false
	}
	markers, err := readers.Markers(ctx, sessionID)
	if err != nil {
		slog.Warn("Could not read agent CLI markers; the branch gets a written copy", "error", err)
		return cliFork{}, false
	}
	marked := map[string]db.MessageMarker{}
	for _, mk := range markers {
		if mk.CLI == string(m.Kind) {
			marked[mk.MessageID] = mk
		}
	}
	at := last
	for at >= 0 && marked[path[at].ID].Marker == "" {
		at--
	}
	if at < 0 {
		return cliFork{}, false
	}
	mk := marked[path[at].ID]
	if cliagent.CanFork(m.Kind, mk.MarkerKind) != nil {
		return cliFork{}, false
	}
	from := slices.IndexFunc(history, func(msg message.Message) bool { return msg.ID == path[at].ID })
	to := slices.IndexFunc(history, func(msg message.Message) bool { return msg.ID == path[last].ID })
	if from < 0 || to < from {
		return cliFork{}, false
	}
	link := cliagent.Link{Native: mk.NativeSession, Through: path[last].ID}
	if source := m.Links.Get(origin.ParentSessionID, m.Kind); source.Native == mk.NativeSession {
		// The fork keeps what its source session was already told.
		link.Tasks, link.TaskInstructionsHash, link.SharedInstructions = source.Tasks, source.TaskInstructionsHash, source.SharedInstructions
	}
	point := cliagent.ForkPoint{Native: mk.NativeSession, Marker: mk.Marker, MarkerKind: mk.MarkerKind}
	return cliFork{point: point, link: link, note: history[from+1 : to+1]}, true
}

// branchCut returns the branch's path and the index on it of the last
// message the branch copied: the copy of the cut message, or of the last
// tool message answering its tool calls. The copies start the path in the
// source's order, so the cut's copy sits at the cut's depth in its source
// chat; its role and time confirm it.
func (a *sessionAgent) branchCut(ctx context.Context, sessionID string, origin db.ChatLink) ([]message.Message, int, bool) {
	tree, ok := a.messages.(message.TreeService)
	if !ok {
		return nil, 0, false
	}
	entries, err := tree.Tree(ctx, origin.ParentSessionID)
	if err != nil {
		return nil, 0, false
	}
	parents := make(map[string]string, len(entries))
	for _, e := range entries {
		parents[e.MessageID] = e.ParentID
	}
	depth := 0
	for id := origin.SourceMessageID; id != ""; depth++ {
		parent, ok := parents[id]
		if !ok || depth > len(entries) {
			return nil, 0, false
		}
		id = parent
	}
	source, err := a.messages.Get(ctx, origin.SourceMessageID)
	if err != nil {
		return nil, 0, false
	}
	path, err := a.messages.List(ctx, sessionID)
	if err != nil || depth == 0 || depth > len(path) {
		return nil, 0, false
	}
	last := depth - 1
	if path[last].Role != source.Role || path[last].CreatedAt != source.CreatedAt {
		return nil, 0, false
	}
	pending := map[string]bool{}
	for _, call := range path[last].ToolCalls() {
		if !call.ProviderExecuted {
			pending[call.ID] = true
		}
	}
	for len(pending) > 0 && last+1 < len(path) && path[last+1].Role == message.Tool {
		answered := false
		for _, result := range path[last+1].ToolResults() {
			if pending[result.ToolCallID] {
				delete(pending, result.ToolCallID)
				answered = true
			}
		}
		if !answered {
			break
		}
		last++
	}
	return path, last, true
}

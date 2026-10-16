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
func (a *sessionAgent) cliStream(m *cliagent.Model, call SessionAgentCall, history []message.Message, effort string, onCompacting func(bool) error, onActivity func(activity, detail string) error) func(context.Context, fantasy.AgentStreamCall) (*fantasy.AgentResult, error) {
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
		prompt, err := cliPromptWithAttachments(call.Prompt, call.Attachments)
		if err != nil {
			return nil, err
		}
		text, resume := cliHandoff(history, link, prompt)
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

		s := &cliSteps{call: call, onQueuedInput: call.onQueuedInput, ctx: ctx, sc: sc, m: m, a: a, sessionID: call.SessionID, onCompacting: onCompacting, onActivity: onActivity, steerReady: make(chan struct{}, 1)}
		if err := s.begin(); err != nil {
			return nil, err
		}
		a.steering.Set(call.SessionID, s)
		defer a.steering.CompareAndDelete(call.SessionID, s)
		defer s.returnUnsteered()
		// Prompts sent mid-turn go in at the next tool boundary (see
		// steerCalls), or wait for the turn to end if no tool runs.
		turn := cliagent.Turn{SessionID: call.SessionID, Prompt: text, Continue: call.CLIContinue, Attachments: call.Attachments, Resume: resume, Effort: effort, Emit: s.handle, Steer: s.steer, SteerInput: s.steerWithImages, SteerReady: s.steerReady, Env: env, Instructions: instructions}
		err = m.Run(ctx, turn)
		if errors.Is(err, cliagent.ErrResume) {
			// The native session is gone; hand the whole conversation to a
			// fresh one instead.
			slog.Warn("Agent CLI could not resume its session; starting fresh", "cli", m.Kind, "session", resume)
			turn.Prompt, turn.Resume = cliHandoff(history, cliagent.Link{}, prompt)
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
	text, resume := cliHandoff(history, link, prompt)
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

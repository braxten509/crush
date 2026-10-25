package agent

import (
	"context"
	"slices"
	"time"

	"charm.land/fantasy"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/message"
)

// A sub-agent's own chat (owner, 2026-10-21): the desktop opens a helper's
// child session as a normal chat. What the user types there goes to the
// sub-agent, not the main agent: into its running turn, as any chat's
// mid-reply message, or once it is done as a follow-up, as `crush spawn
// --continue` sends one. Either way its result still goes to the parent.
// Stop in that chat stops the sub-agent as the user's stop does.

// taskRun is what a running task's chat messages need: the agent it runs on
// and the reports of the messages sent to it, which the task waits for
// before it hands its result on.
type taskRun struct {
	ctx      context.Context
	sub      SessionAgent
	model    Model
	provider config.ProviderConfig
	pending  []*reportBox
}

// chatCall carries an app's prompt IDs into the first turn of a follow-up,
// so the app sees its prompt end.
type chatCall struct {
	submissionID string
	runID        string
}

// childTask finds the task whose child session is childID: a running one
// first, then the latest this Crush knows, then the listed ones.
func (h *taskHub) childTask(childID string) (Task, bool) {
	h.mu.Lock()
	var found *Task
	for _, t := range h.tasks {
		if t.ChildID != childID {
			continue
		}
		if found == nil || t.Status == TaskRunning || found.Status != TaskRunning && t.Started.After(found.Started) {
			found = t
		}
	}
	if found != nil {
		t := *found
		h.mu.Unlock()
		return t, true
	}
	h.mu.Unlock()
	var listed *savedTask
	_ = editTaskFile(h.historyPath(), func(saved []savedTask) []savedTask {
		if i := slices.IndexFunc(saved, func(s savedTask) bool { return s.ChildID == childID }); i >= 0 {
			listed = &saved[i]
		}
		return saved
	})
	if listed == nil {
		return Task{}, false
	}
	return listed.Task, true
}

// chatMessage sends an app's prompt in a sub-agent's chat to the sub-agent.
// ok is false when sessionID is no sub-agent's chat.
func (h *taskHub) chatMessage(ctx context.Context, accept *AcceptedRun, sessionID, prompt string, attachments []message.Attachment) (result *fantasy.AgentResult, ok bool, err error) {
	t, found := h.childTask(sessionID)
	if !found {
		return nil, false, nil
	}
	// The reservation is the main agent's; the sub-agent queues on its own.
	accept.Close()
	ids := chatCall{submissionID: message.SubmissionID(ctx), runID: RunIDFromContext(ctx)}

	// The fallback order moves a sub-agent under h.mu, so its agent and
	// model are read while it is held.
	h.mu.Lock()
	run := h.runs[t.ID]
	var box *reportBox
	var current taskRun
	if run != nil {
		box = newReportBox()
		run.pending = append(run.pending, box)
		current = taskRun{ctx: run.ctx, sub: run.sub, model: run.model, provider: run.provider}
	}
	h.mu.Unlock()
	if run != nil {
		result, err = current.sub.Run(current.ctx, SessionAgentCall{
			SessionID:       sessionID,
			SubmissionID:    ids.submissionID,
			RunID:           ids.runID,
			Prompt:          prompt,
			Attachments:     attachments,
			MaxOutputTokens: subAgentMaxTokens(current.model),
			NonInteractive:  true,
			ProviderOptions: getProviderOptions(current.model, current.provider),
			report:          box,
		})
		// A queued message's box fills when its turn ends; a turn that
		// ran here fills it now (fill keeps the first).
		if result != nil || err != nil {
			box.fill(result, err)
		}
		MarkRunCompletePublished(ctx)
		return result, true, err
	}

	// Done: wait out the last moments of its end, then follow up.
	for end := time.Now().Add(5 * time.Second); h.childRunning(sessionID) && time.Now().Before(end); {
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := h.continueTask(TaskRequest{Session: t.SessionID, Continue: t.ID, Prompt: prompt, chat: &ids}); err != nil {
		return nil, true, err
	}
	return nil, true, nil
}

// awaitChatMessages waits for the turns of the messages the user sent in
// the task's chat while it ran; the last one's result is the task's.
func (h *taskHub) awaitChatMessages(ctx context.Context, id string, result *fantasy.AgentResult, err error) (*fantasy.AgentResult, error) {
	for {
		h.mu.Lock()
		run := h.runs[id]
		var pending []*reportBox
		if run != nil {
			pending, run.pending = run.pending, nil
		}
		if len(pending) == 0 {
			delete(h.runs, id)
			h.mu.Unlock()
			return result, err
		}
		h.mu.Unlock()
		for _, box := range pending {
			select {
			case <-box.done:
				if box.result != nil || box.err != nil {
					result, err = box.result, box.err
				}
			case <-ctx.Done():
				h.mu.Lock()
				delete(h.runs, id)
				h.mu.Unlock()
				return result, ctx.Err()
			}
		}
	}
}

// stopChild stops the running task whose chat is childID, as the user's
// stop does: its parent is told. It reports whether one ran.
func (h *taskHub) stopChild(childID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for id, t := range h.tasks {
		if t.ChildID == childID && t.Status == TaskRunning {
			t.Status = TaskStopped
			h.userStopped[id] = true
			h.cancels[id]()
			return true
		}
	}
	return false
}

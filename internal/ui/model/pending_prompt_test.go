package model

import (
	"context"
	"errors"
	"testing"

	"github.com/charmbracelet/crush/internal/agent/notify"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/charmbracelet/crush/internal/ui/chat"
	"github.com/stretchr/testify/require"
)

type submissionWorkspace struct {
	*countingWorkspace
	submissions []string
	err         error
	saved       []message.Message
}

func (w *submissionWorkspace) AgentRun(ctx context.Context, sessionID, prompt string, attachments ...message.Attachment) error {
	w.submissions = append(w.submissions, message.SubmissionID(ctx))
	return w.err
}

func (w *submissionWorkspace) ListMessages(context.Context, string) ([]message.Message, error) {
	return w.saved, nil
}

func newSubmissionUI() (*UI, *submissionWorkspace) {
	u, base := newRecallUI()
	ws := &submissionWorkspace{countingWorkspace: base.countingWorkspace}
	u.com.Workspace = ws
	// Start idle so a send shows in the chat at once; prompts sent
	// mid-turn are held in the queue list instead (see held_prompt_test.go).
	warmCaches(u, false)
	return u, ws
}

func TestSendingShowsMessageBeforeAgentReceivesIt(t *testing.T) {
	u, ws := newSubmissionUI()
	attachment := message.Attachment{FileName: "photo.png", MimeType: "image/png", Content: []byte{1, 2, 3}}
	cmd := u.sendMessage("instant", attachment)
	require.Empty(t, ws.submissions)
	require.Len(t, u.chat.flat, 1)
	require.Contains(t, u.chat.flat[0].RawRender(100), "instant")
	require.Len(t, u.pendingPrompts[0].BinaryContent(), 1)
	id := u.pendingPrompts[0].ID
	runCmds(u, cmd)
	require.Equal(t, []string{id}, ws.submissions)
	confirmed := u.pendingPrompts[0].Clone()
	confirmed.ID = "saved"
	u.appendSessionMessage(confirmed)
	require.Len(t, u.chat.flat, 1, "confirmation must replace, not duplicate, the submitted message")
	require.Equal(t, "saved", u.chat.flat[0].ID())
	require.Empty(t, u.pendingPrompts)
}

func TestIdenticalSubmissionsReconcileIndependently(t *testing.T) {
	u, _ := newSubmissionUI()
	u.sendMessage("same")
	u.sendMessage("same")
	first, second := u.pendingPrompts[0].Clone(), u.pendingPrompts[1].Clone()
	first.ID, second.ID = "first", "second"
	u.appendSessionMessage(second)
	u.appendSessionMessage(first)
	require.Len(t, u.chat.flat, 2)
	require.Equal(t, "first", u.chat.flat[0].ID())
	require.Equal(t, "second", u.chat.flat[1].ID())
}

func TestPendingMessagesSurviveSessionReloadWithoutDuplicates(t *testing.T) {
	u, _ := newSubmissionUI()
	u.sendMessage("pending")
	u.setSessionMessages(nil)
	require.Len(t, u.chat.flat, 1)
	confirmed := u.pendingPrompts[0].Clone()
	confirmed.ID = "saved"
	u.setSessionMessages([]message.Message{confirmed})
	require.Len(t, u.chat.flat, 1)
	require.Equal(t, "saved", u.chat.flat[0].ID())
	require.Empty(t, u.pendingPrompts)
}

func TestFailedSubmissionRestoresDraftAndAttachments(t *testing.T) {
	u, ws := newSubmissionUI()
	ws.err = errors.New("send failed")
	cmd := u.sendMessage("retry me", message.Attachment{FileName: "photo.png", MimeType: "image/png", Content: []byte{1}})
	u.textarea.SetValue("new draft")
	id := u.pendingPrompts[0].ID
	runCmds(u, cmd)
	require.Len(t, u.pendingPrompts, 1, "a failed acknowledgement may still be saved later")
	_, cmd = u.Update(pubsub.Event[notify.RunComplete]{Type: pubsub.UpdatedEvent, Payload: notify.RunComplete{SessionID: "s1", SubmissionID: id, Error: "send failed"}})
	runCmds(u, cmd)
	require.Empty(t, u.pendingPrompts)
	require.Empty(t, u.chat.flat)
	require.Equal(t, "new draft\n\nretry me", u.textarea.Value())
	require.Len(t, u.attachments.List(), 1)
}

func TestDefinitiveLocalFailureRestoresUnsentPrompt(t *testing.T) {
	u, ws := newSubmissionUI()
	ws.err = &message.DefinitiveSubmissionError{Err: errors.New("model refresh failed")}
	cmd := u.sendMessage("retry me", message.Attachment{FileName: "photo.png", MimeType: "image/png", Content: []byte{1}})
	runCmds(u, cmd)
	require.Empty(t, u.pendingPrompts)
	require.Empty(t, u.chat.flat)
	require.Equal(t, "retry me", u.textarea.Value())
	require.Len(t, u.attachments.List(), 1)
}

func TestDefinitiveLocalFailureKeepsSavedMessageOutOfDraft(t *testing.T) {
	u, ws := newSubmissionUI()
	ws.err = &message.DefinitiveSubmissionError{Err: errors.New("model error after save")}
	cmd := u.sendMessage("saved")
	saved := u.pendingPrompts[0].Clone()
	saved.ID = "saved"
	ws.saved = []message.Message{saved}
	runCmds(u, cmd)
	require.Empty(t, u.textarea.Value())
	require.Empty(t, u.pendingPrompts)
	require.Len(t, u.chat.flat, 1)
	require.Equal(t, "saved", u.chat.flat[0].ID())
}

func TestRecallRemovesImmediateTimelineCopy(t *testing.T) {
	u, _ := newSubmissionUI()
	u.sendMessage("queued")
	id := u.pendingPrompts[0].ID
	u.applyRecalledPrompt(queuedPromptRecalledMsg{sessionID: "s1", prompt: &message.QueuedPrompt{Prompt: "queued", SubmissionID: id}})
	require.Empty(t, u.chat.flat)
	require.Empty(t, u.pendingPrompts)
	require.Equal(t, "queued", u.textarea.Value())
}

func TestCancelShowsImmediatelyAndLateEventsStayCanceled(t *testing.T) {
	u, ws := newSubmissionUI()
	active := message.Message{ID: "assistant", SessionID: "s1", Role: message.Assistant,
		Parts: []message.ContentPart{message.ToolCall{ID: "tool", Name: "bash", Input: `{"command":"sleep 10"}`, Finished: true}}}
	u.appendSessionMessage(active)
	tool := u.chat.MessageItem("tool").(chat.ToolMessageItem)
	cmd := u.interruptAgent()
	require.Zero(t, ws.interruptCalls)
	require.Equal(t, chat.ToolStatusCanceled, tool.Status())
	u.Update(pubsub.Event[message.Message]{Type: pubsub.UpdatedEvent, Payload: active})
	require.Equal(t, chat.ToolStatusCanceled, tool.Status(), "a buffered update must not resurrect the action")
	active.Parts = append(active.Parts, message.ToolCall{ID: "late-tool", Name: "bash", Input: `{"command":"sleep 10"}`})
	u.updateSessionMessage(active)
	require.Equal(t, chat.ToolStatusCanceled, u.chat.MessageItem("late-tool").(chat.ToolMessageItem).Status())
	u.setSessionMessages([]message.Message{active})
	require.Equal(t, chat.ToolStatusCanceled, u.chat.MessageItem("tool").(chat.ToolMessageItem).Status(), "reload must preserve the cancellation overlay")
	terminal := active.Clone()
	terminal.AddFinish(message.FinishReasonCanceled, "", "")
	u.updateSessionMessage(terminal)
	u.setSessionMessages([]message.Message{active})
	require.Equal(t, chat.ToolStatusCanceled, u.chat.MessageItem("tool").(chat.ToolMessageItem).Status(), "a stale reload after the terminal event must also stay canceled")
	require.Zero(t, ws.interruptCalls)
	require.NotNil(t, cmd)
	require.IsType(t, agentInterruptedMsg{}, cmd())
	require.Equal(t, 1, ws.interruptCalls)
}

func TestCancelLeavesFinishedActionsAlone(t *testing.T) {
	u, _ := newSubmissionUI()
	finished := message.Message{ID: "done", SessionID: "s1", Role: message.Assistant,
		Parts: []message.ContentPart{message.ToolCall{ID: "tool", Name: "view", Finished: true}}}
	u.appendSessionMessage(finished)
	u.appendSessionMessage(message.Message{ID: "result", SessionID: "s1", Role: message.Tool,
		Parts: []message.ContentPart{message.ToolResult{ToolCallID: "tool", Name: "view", Content: "done"}}})
	tool := u.chat.MessageItem("tool").(chat.ToolMessageItem)
	status := tool.Status()
	u.interruptAgent()
	require.Equal(t, status, tool.Status())
}

func TestCompletionBeforeConfirmationDoesNotDuplicateDraft(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		u, ws := newSubmissionUI()
		u.sendMessage("already saved")
		id := u.pendingPrompts[0].ID
		saved := u.pendingPrompts[0].Clone()
		saved.ID = "saved"
		ws.saved = []message.Message{saved}
		_, cmd := u.Update(pubsub.Event[notify.RunComplete]{Type: pubsub.UpdatedEvent,
			Payload: notify.RunComplete{SessionID: "s1", SubmissionID: id, Error: "run ended", Cancelled: canceled}})
		runCmds(u, cmd)
		u.appendSessionMessage(saved)
		require.Empty(t, u.textarea.Value(), "saved messages must not also reappear as unsent drafts")
		require.Len(t, u.chat.flat, 1)
		require.Equal(t, "saved", u.chat.flat[0].ID())
		require.Empty(t, u.pendingPrompts)
	}
}

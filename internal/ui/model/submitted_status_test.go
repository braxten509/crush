package model

import (
	tea "charm.land/bubbletea/v2"
	"context"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/skills"
	"github.com/charmbracelet/crush/internal/ui/dialog"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestSubmittedMessageShowsWorkingBeforeDispatchAndHandsOff(t *testing.T) {
	u, ws := newSubmissionUI()
	u.chat.SetSize(100, 30)
	u.sendMessage("instant")
	require.Empty(t, ws.submissions)
	require.Len(t, u.chat.flat, 1, "Working is display-only")
	require.Contains(t, ansi.Strip(u.chat.list.Render()), "Working")
	require.NotNil(t, u.chat.EnsureAnimating())
	confirmed := u.pendingPrompts[0].Clone()
	confirmed.ID = "saved"
	u.appendSessionMessage(confirmed)
	require.Contains(t, ansi.Strip(u.chat.list.Render()), "Working")
	u.appendSessionMessage(message.Message{ID: "assistant", SessionID: "s1", Role: message.Assistant})
	require.Nil(t, u.chat.waitingItem, "the real assistant replaces the temporary status")
	require.Contains(t, ansi.Strip(u.chat.list.Render()), "Working")
	u.chat.SetAgentBusy(false)
}

func TestLegacyQueueSummariesHideOnlyMatchingPendingCopies(t *testing.T) {
	u, _ := newSubmissionUI()
	u.sendMessage("same")
	id := u.pendingPrompts[0].ID
	for _, entries := range [][]message.QueuedPromptSummary{
		{{Prompt: "same"}, {Prompt: "same"}},
		{{Prompt: "same", SubmissionID: id}, {Prompt: "same"}},
		{{Prompt: "same"}, {Prompt: "same", SubmissionID: id}},
	} {
		u.promptQueueItems = []string{"same", "same"}
		u.promptQueueEntries = entries
		require.Equal(t, []string{"same"}, u.visiblePromptQueueItems())
	}
}

type sessionSpecificWorkspace struct{ *countingWorkspace }

func (w *sessionSpecificWorkspace) AgentIsBusy() bool                 { return true }
func (w *sessionSpecificWorkspace) AgentIsSessionBusy(id string) bool { return id == "other-active" }

func TestChatBusyProbeDoesNotInheritWorkFromAnotherSession(t *testing.T) {
	base := &countingWorkspace{ready: true}
	u := newBusyUI(base)
	u.com.Workspace = &sessionSpecificWorkspace{base}
	u.appendSessionMessage(message.Message{ID: "last-user", SessionID: "s1", Role: message.User})
	u.chat.SetAgentBusy(true)
	msg := u.dispatchBusyRefresh()().(busyStateMsg)
	require.False(t, msg.agentBusy)
	u.applyBusyState(msg)
	require.Nil(t, u.chat.waitingItem)
}

type statusSkillManager struct{}

func (statusSkillManager) ListInstalledSkills(context.Context) ([]skills.InstalledSkill, error) {
	return []skills.InstalledSkill{{CatalogEntry: skills.CatalogEntry{ID: "example", Name: "example"}, Enabled: true}}, nil
}
func (statusSkillManager) SetSkillEnabled(context.Context, string, bool) error       { return nil }
func (statusSkillManager) InstallSkill(context.Context, skills.DirectorySkill) error { return nil }

type coveringDialog struct{}

func (coveringDialog) ID() string                               { return "cover" }
func (coveringDialog) HandleMsg(tea.Msg) dialog.Action          { return nil }
func (coveringDialog) Draw(uv.Screen, uv.Rectangle) *tea.Cursor { return nil }

func TestSkillCompletionReachesPickerUnderAnotherDialog(t *testing.T) {
	u, _ := newSubmissionUI()
	picker := dialog.NewSkills(u.com, statusSkillManager{})
	msg := picker.Load()()
	u.dialog.OpenDialog(picker)
	u.dialog.OpenDialog(coveringDialog{})
	u.Update(msg)
	require.Equal(t, "cover", u.dialog.DialogLast().ID())
	action := picker.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.IsType(t, dialog.ActionCmd{}, action, "the buried picker must finish loading and accept a toggle")
	picker.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEscape})
}

func TestCanceledPendingMessageDropsWorkingRow(t *testing.T) {
	u, _ := newSubmissionUI()
	warmCaches(u, false)
	u.chat.SetSize(100, 30)
	u.sendMessage("instant")
	require.NotNil(t, u.chat.waitingItem)
	u.interruptAgent()
	require.Nil(t, u.chat.waitingItem)
	require.NotContains(t, ansi.Strip(u.chat.list.Render()), "Working")
}

func TestSlowDispatchDoesNotLoseWorkingToAnIdleProbe(t *testing.T) {
	u, ws := newSubmissionUI()
	u.chat.SetSize(100, 30)
	u.sendMessage("still sending")
	u.applyBusyState(busyStateMsg{gen: u.busyFetchGen, ready: true, agentBusy: false})
	require.Empty(t, ws.submissions)
	require.True(t, u.isAgentBusy())
	require.Contains(t, ansi.Strip(u.chat.list.Render()), "Working")
}

func TestCancelBeforeDispatchPreventsThePromptBeingSent(t *testing.T) {
	u, ws := newSubmissionUI()
	warmCaches(u, false)
	cmd := u.sendMessage("cancel before handoff")
	runCmds(u, u.interruptAgent())
	runCmds(u, cmd)
	require.Empty(t, ws.submissions)
	require.Empty(t, u.submittingPrompts)
	require.Nil(t, u.chat.waitingItem)
}

type slowSubmissionWorkspace struct {
	*submissionWorkspace
	started chan struct{}
}

func (w *slowSubmissionWorkspace) AgentRun(ctx context.Context, _ string, _ string, _ ...message.Attachment) error {
	close(w.started)
	<-ctx.Done()
	return ctx.Err()
}

func TestCancelDuringSlowHandoffCancelsDispatchContext(t *testing.T) {
	u, base := newSubmissionUI()
	warmCaches(u, false)
	ws := &slowSubmissionWorkspace{submissionWorkspace: base, started: make(chan struct{})}
	u.com.Workspace = ws
	commands := u.sendMessage("slow handoff")().(tea.BatchMsg)
	done := make(chan tea.Msg, 1)
	go func() { done <- commands[len(commands)-1]() }()
	select {
	case <-ws.started:
	case <-time.After(time.Second):
		t.Fatal("submission did not start")
	}
	runCmds(u, u.interruptAgent())
	select {
	case msg := <-done:
		require.ErrorIs(t, msg.(agentRunSubmittedMsg).err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("cancel did not stop dispatch")
	}
}

func TestLegacyQueueConfirmationDoesNotRestoreADuplicate(t *testing.T) {
	u, _ := newSubmissionUI()
	u.sendMessage("same")
	u.promptQueueItems = []string{"same", "same"}
	u.promptQueue = 2
	u.promptQueueEntries = []message.QueuedPromptSummary{{Prompt: "same"}, {Prompt: "same"}}
	confirmed := u.pendingPrompts[0].Clone()
	confirmed.ID = "saved"
	u.appendSessionMessage(confirmed)
	require.Empty(t, u.pendingPrompts)
	require.Equal(t, []string{"same"}, u.visiblePromptQueueItems())
	u.applyPromptQueue(promptQueueMsg{forSession: "s1", gen: u.promptQueueGen, prompts: []string{"same"}, entries: []message.QueuedPromptSummary{{Prompt: "same"}}})
	require.Equal(t, []string{"same"}, u.visiblePromptQueueItems(), "the remaining different submission remains visible in a fresh snapshot")
}

func TestSentMessagesAreNotRepeatedInQueueEvenWithIdenticalText(t *testing.T) {
	u, _ := newSubmissionUI()
	u.sendMessage("same")
	id := u.pendingPrompts[0].ID
	u.applyPromptQueue(promptQueueMsg{forSession: "s1", gen: u.promptQueueGen,
		prompts: []string{"same", "same"}, entries: []message.QueuedPromptSummary{{Prompt: "same", SubmissionID: id}, {Prompt: "same", SubmissionID: "different"}}})
	require.Equal(t, 2, u.promptQueue, "internal delivery state is preserved")
	require.Equal(t, []string{"same"}, u.visiblePromptQueueItems(), "only the different submission stays in the queue panel")
	confirmed := u.pendingPrompts[0].Clone()
	confirmed.ID = "saved"
	u.appendSessionMessage(confirmed)
	require.Equal(t, 1, u.visiblePromptQueueCount(), "confirmation still uses the same submission identity")
	u.applyPromptQueue(promptQueueMsg{forSession: "s1", gen: u.promptQueueGen, prompts: []string{"same"}, entries: []message.QueuedPromptSummary{{Prompt: "same", SubmissionID: id}}})
	require.Zero(t, u.visiblePromptQueueCount())
	require.NotContains(t, ansi.Strip(u.pillsView), "Queued")
}

package model

import (
	"context"
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/ui/completions"
	"github.com/stretchr/testify/require"
)

type recallWorkspace struct {
	*countingWorkspace
	prompt      *message.QueuedPrompt
	err         error
	recallCalls int
}

func (w *recallWorkspace) AgentRecallQueuedPrompt(context.Context, string) (*message.QueuedPrompt, error) {
	w.recallCalls++
	prompt := w.prompt
	w.prompt = nil
	return prompt, w.err
}

func newRecallUI() (*UI, *recallWorkspace) {
	ws := &recallWorkspace{countingWorkspace: &countingWorkspace{ready: true, agentBusy: true}}
	u := newBusyUI(ws.countingWorkspace)
	u.com.Workspace = ws
	u.completions = completions.New(u.com.Styles.Completions.Normal, u.com.Styles.Completions.Focused, u.com.Styles.Completions.Match)
	u.textarea.CharLimit = -1
	u.promptHistory.index = -1
	u.promptHistory.messages = []string{"past message"}
	warmCaches(u, true)
	return u, ws
}

func TestUpRecallsQueuedPromptBeforeHistory(t *testing.T) {
	u, ws := newRecallUI()
	attachment := message.Attachment{FileName: "image.png", MimeType: "image/png", Content: []byte{1, 2}}
	ws.prompt = &message.QueuedPrompt{Prompt: "queued message", Attachments: []message.Attachment{attachment}}
	// The UI count is still zero: consult the actual queue asynchronously.
	cmd := u.handleHistoryUp(tea.KeyPressMsg{Code: tea.KeyUp})
	require.NotNil(t, cmd)
	require.Zero(t, ws.recallCalls, "Up must not make a synchronous workspace request")
	require.Nil(t, u.handleHistoryUp(tea.KeyPressMsg{Code: tea.KeyUp}), "repeated Up must not start a second recall")
	msg := cmd().(queuedPromptRecalledMsg)
	u.applyRecalledPrompt(msg)
	require.Equal(t, "queued message", u.textarea.Value())
	require.Equal(t, -1, u.promptHistory.index)
	require.Equal(t, []message.Attachment{attachment}, u.attachments.List())
	require.Zero(t, ws.cancelCalls)
	require.Zero(t, ws.interruptCalls)
	require.Zero(t, ws.clearQueueCalls)
}

func TestUpFallsBackToHistoryWhenQueueCannotBeRecalled(t *testing.T) {
	u, _ := newRecallUI()
	cmd := u.handleHistoryUp(tea.KeyPressMsg{Code: tea.KeyUp})
	u.applyRecalledPrompt(cmd().(queuedPromptRecalledMsg))
	require.Equal(t, "past message", u.textarea.Value())
	require.Equal(t, 0, u.promptHistory.index)
}

func TestUpRecallPreservesConcurrentDraft(t *testing.T) {
	for _, available := range []bool{false, true} {
		u, ws := newRecallUI()
		if available {
			ws.prompt = &message.QueuedPrompt{Prompt: "queued message"}
		}
		cmd := u.handleHistoryUp(tea.KeyPressMsg{Code: tea.KeyUp})
		u.textarea.SetValue("new draft")
		u.applyRecalledPrompt(cmd().(queuedPromptRecalledMsg))
		want := "new draft"
		if available {
			want += "\n\nqueued message"
		}
		require.Equal(t, want, u.textarea.Value())
		require.Equal(t, -1, u.promptHistory.index)
	}
}

func TestUpRecallStaysWithOriginalSession(t *testing.T) {
	u, ws := newRecallUI()
	ws.prompt = &message.QueuedPrompt{Prompt: "original chat queued message"}
	cmd := u.handleHistoryUp(tea.KeyPressMsg{Code: tea.KeyUp})
	u.session = &session.Session{ID: "other"}
	u.textarea.SetValue("other chat draft")
	u.applyRecalledPrompt(cmd().(queuedPromptRecalledMsg))
	require.Equal(t, "other chat draft", u.textarea.Value())
	u.session = &session.Session{ID: "s1"}
	u.textarea.Reset()
	u.restoreRecalledDrafts()
	require.Equal(t, "original chat queued message", u.textarea.Value())
}

func TestUpRecallErrorPreservesEditorAndQueue(t *testing.T) {
	u, ws := newRecallUI()
	ws.err = errors.New("server unavailable")
	u.textarea.SetValue("draft")
	u.textarea.MoveToBegin()
	cmd := u.handleHistoryUp(tea.KeyPressMsg{Code: tea.KeyUp})
	require.NotNil(t, u.applyRecalledPrompt(cmd().(queuedPromptRecalledMsg)))
	require.Equal(t, "draft", u.textarea.Value())
	require.Equal(t, -1, u.promptHistory.index)
	require.False(t, u.promptRecallInFlight)
	// The server may now be idle after withdrawing the last prompt.
	warmCaches(u, false)
	u.promptQueue = 0
	u.textarea.MoveToBegin()
	ws.err = nil
	ws.prompt = &message.QueuedPrompt{Prompt: "recovered"}
	cmd = u.handleHistoryUp(tea.KeyPressMsg{Code: tea.KeyUp})
	u.applyRecalledPrompt(cmd().(queuedPromptRecalledMsg))
	require.Contains(t, u.textarea.Value(), "recovered")
	require.False(t, u.failedRecalls["s1"])
}

func TestUpRecallTakesPriorityWhileBrowsingHistory(t *testing.T) {
	u, ws := newRecallUI()
	u.promptHistory.index = 0
	u.textarea.SetValue("past message")
	u.textarea.MoveToBegin()
	ws.prompt = &message.QueuedPrompt{Prompt: "queued message"}
	cmd := u.handleHistoryUp(tea.KeyPressMsg{Code: tea.KeyUp})
	u.applyRecalledPrompt(cmd().(queuedPromptRecalledMsg))
	require.Contains(t, u.textarea.Value(), "queued message")
	require.Equal(t, -1, u.promptHistory.index)
}

func TestUpRecallDoesNotTurnMessagesIntoShellCommands(t *testing.T) {
	u, ws := newRecallUI()
	ws.prompt = &message.QueuedPrompt{Prompt: "!this is an agent prompt"}
	cmd := u.handleHistoryUp(tea.KeyPressMsg{Code: tea.KeyUp})
	u.applyRecalledPrompt(cmd().(queuedPromptRecalledMsg))
	require.Equal(t, "!this is an agent prompt", u.textarea.Value())
	require.False(t, u.bangMode)
}

func TestUpRecallDefersRestoreIfUserStartsShellCommand(t *testing.T) {
	u, ws := newRecallUI()
	ws.prompt = &message.QueuedPrompt{Prompt: "queued message"}
	cmd := u.handleHistoryUp(tea.KeyPressMsg{Code: tea.KeyUp})
	u.bangMode = true
	u.textarea.SetValue("echo hello")
	u.applyRecalledPrompt(cmd().(queuedPromptRecalledMsg))
	require.Equal(t, "echo hello", u.textarea.Value())
	require.True(t, u.bangMode)
	u.bangMode = false
	u.textarea.Reset()
	u.handleHistoryUp(tea.KeyPressMsg{Code: tea.KeyUp})
	require.Equal(t, "queued message", u.textarea.Value())
}

package model

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/history"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/ui/dialog"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/stretchr/testify/require"
)

// These tests pin the rule that commands never change the model: the work a
// command does off the Update goroutine comes back as a message, and the
// state changes happen when Update applies it. Each runs the command on
// another goroutine while the test reads the same state, so -race flags a
// command that writes (or reads) the model.

// cmdStateWorkspace records config writes and file-tracker lookups on top
// of the busy-test stub.
type cmdStateWorkspace struct {
	countingWorkspace
	cfg *config.Config

	mu       sync.Mutex
	sets     map[string]any
	setErr   error
	lastRead time.Time
	userMsgs []string
}

func (w *cmdStateWorkspace) Config() *config.Config { return w.cfg }

func (w *cmdStateWorkspace) SetConfigField(_ config.Scope, key string, value any) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.setErr != nil {
		return w.setErr
	}
	if w.sets == nil {
		w.sets = make(map[string]any)
	}
	w.sets[key] = value
	return nil
}

func (w *cmdStateWorkspace) set(key string) (any, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	v, ok := w.sets[key]
	return v, ok
}

func (w *cmdStateWorkspace) FileTrackerLastReadTime(context.Context, string, string) time.Time {
	return w.lastRead
}

func (w *cmdStateWorkspace) ListUserMessages(_ context.Context, sessionID string) ([]message.Message, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.userMsgs = append(w.userMsgs, sessionID)
	return nil, nil
}

func newCmdStateUI(t *testing.T) (*UI, *cmdStateWorkspace) {
	t.Helper()
	ws := &cmdStateWorkspace{cfg: &config.Config{Options: &config.Options{TUI: &config.TUIOptions{}}}}
	u := newBusyUI(&ws.countingWorkspace)
	u.com.Workspace = ws
	return u, ws
}

// runOffLoop runs cmd on another goroutine, as the Bubble Tea runtime does,
// while read touches the model on this one. It returns every leaf message
// the command tree produced.
func runOffLoop(t *testing.T, cmd tea.Cmd, read func()) []tea.Msg {
	t.Helper()
	var msgs []tea.Msg
	done := make(chan struct{})
	go func() {
		defer close(done)
		msgs = collectMsgs(cmd)
	}()
	for {
		select {
		case <-done:
			return msgs
		default:
			read()
		}
	}
}

// collectMsgs executes a command tree and returns its leaf messages.
func collectMsgs(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		var out []tea.Msg
		for _, c := range msg {
			out = append(out, collectMsgs(c)...)
		}
		return out
	case nil:
		return nil
	default:
		return []tea.Msg{msg}
	}
}

func findMsg[T any](msgs []tea.Msg) (T, bool) {
	for _, msg := range msgs {
		if typed, ok := msg.(T); ok {
			return typed, true
		}
	}
	var zero T
	return zero, false
}

func TestToggleTransparentBackgroundAppliesOnUpdate(t *testing.T) {
	t.Parallel()

	u, ws := newCmdStateUI(t)
	cmd := u.handleDialogAction(dialog.ActionToggleTransparentBackground{})
	seen := false
	msgs := runOffLoop(t, cmd, func() { seen = seen || u.isTransparent })
	require.False(t, seen)

	require.False(t, u.isTransparent, "the command must not change the model")
	v, ok := ws.set("options.tui.transparent")
	require.True(t, ok)
	require.Equal(t, true, v)

	written, ok := findMsg[configWriteMsg](msgs)
	require.True(t, ok)
	u.Update(written)
	require.True(t, u.isTransparent)
}

func TestToggleTransparentBackgroundKeepsStateOnError(t *testing.T) {
	t.Parallel()

	u, ws := newCmdStateUI(t)
	ws.setErr = os.ErrPermission
	msgs := collectMsgs(u.handleDialogAction(dialog.ActionToggleTransparentBackground{}))
	written, ok := findMsg[configWriteMsg](msgs)
	require.True(t, ok)
	_, cmd := u.Update(written)
	require.False(t, u.isTransparent)
	reported := collectMsgs(cmd)
	require.NotEmpty(t, reported)
}

func TestInsertFileCompletionRecordsReadOnUpdate(t *testing.T) {
	t.Parallel()

	u, _ := newCmdStateUI(t)
	u.session = nil
	path := filepath.Join(t.TempDir(), "notes.txt")
	require.NoError(t, os.WriteFile(path, []byte("hello"), 0o644))
	abs, err := filepath.Abs(path)
	require.NoError(t, err)

	u.textarea.SetValue("@no")
	u.mention.startIndex = 0
	cmd := u.insertFileCompletion(path)
	reads := 0
	msgs := runOffLoop(t, cmd, func() { reads = max(reads, len(u.sessionFileReads)) })
	require.Zero(t, reads)

	require.Empty(t, u.sessionFileReads, "the command must not record the read")
	require.Zero(t, u.attachments.Count())

	picked, ok := findMsg[fileCompletionMsg](msgs)
	require.True(t, ok)
	u.Update(picked)
	require.Equal(t, []string{abs}, u.sessionFileReads)
	require.Equal(t, 1, u.attachments.Count())

	// Picking the same file again before the chat exists attaches it once.
	u.Update(picked)
	require.Equal(t, []string{abs}, u.sessionFileReads)
	require.Equal(t, 1, u.attachments.Count())
}

func TestInsertFileCompletionCapturesSession(t *testing.T) {
	t.Parallel()

	u, ws := newCmdStateUI(t)
	path := filepath.Join(t.TempDir(), "notes.txt")
	require.NoError(t, os.WriteFile(path, []byte("hello"), 0o644))
	// Read after the last change: the attachment is skipped.
	ws.lastRead = time.Now().Add(time.Hour)

	u.textarea.SetValue("@no")
	cmd := u.insertFileCompletion(path)
	msgs := runOffLoop(t, cmd, func() { u.session = &session.Session{ID: "s1"} })
	_, ok := findMsg[fileCompletionMsg](msgs)
	require.False(t, ok, "an unchanged file that was read already is not attached")
}

func TestFilePickerSelectionClosesPickerOnUpdate(t *testing.T) {
	t.Parallel()

	u, _ := newCmdStateUI(t)
	u.dialog.OpenDialog(idDialog{id: dialog.FilePickerID})
	cmd := u.handleDialogAction(dialog.ActionFilePickerSelected{})
	closed := false
	runOffLoop(t, cmd, func() { closed = closed || !u.dialog.ContainsDialog(dialog.FilePickerID) })
	require.False(t, closed)
	require.True(t, u.dialog.ContainsDialog(dialog.FilePickerID), "the command must not close the dialog")

	u.Update(filePickerDoneMsg{})
	require.False(t, u.dialog.ContainsDialog(dialog.FilePickerID))
}

func TestLoadPromptHistoryCapturesSession(t *testing.T) {
	t.Parallel()

	u, ws := newCmdStateUI(t)
	cmd := u.loadPromptHistory()
	runOffLoop(t, cmd, func() { u.session = &session.Session{ID: "s2"} })
	require.Equal(t, []string{"s1"}, ws.userMsgs)
}

func TestHandleFileEventCapturesSession(t *testing.T) {
	t.Parallel()

	u, _ := newCmdStateUI(t)
	ws := &fileHistoryWorkspace{cmdStateWorkspace: u.com.Workspace.(*cmdStateWorkspace)}
	u.com.Workspace = ws
	cmd := u.handleFileEvent(historyFile("s1"))
	require.NotNil(t, cmd)
	runOffLoop(t, cmd, func() { u.session = &session.Session{ID: "s2"} })
	require.Equal(t, []string{"s1"}, ws.listed)
}

func TestPasteImageFromClipboardCapturesModel(t *testing.T) {
	t.Parallel()

	u, ws := newCmdStateUI(t)
	// Without image support the command answers before touching the
	// clipboard; it must not read the model to find that out.
	cmd := u.pasteImageFromClipboard()
	msgs := runOffLoop(t, cmd, func() { ws.cfg = &config.Config{} })
	require.Len(t, msgs, 1)
}

// idDialog is a dialog with a fixed ID and no behaviour.
type idDialog struct{ id string }

func (d idDialog) ID() string                             { return d.id }
func (idDialog) HandleMsg(tea.Msg) dialog.Action          { return nil }
func (idDialog) Draw(uv.Screen, uv.Rectangle) *tea.Cursor { return nil }

// fileHistoryWorkspace serves an empty file history and records the sessions
// it was asked about.
type fileHistoryWorkspace struct {
	*cmdStateWorkspace
	listed []string
}

func (w *fileHistoryWorkspace) ListSessionHistory(_ context.Context, sessionID string) ([]history.File, error) {
	w.listed = append(w.listed, sessionID)
	return nil, nil
}

func historyFile(sessionID string) history.File {
	return history.File{SessionID: sessionID, Path: "a.go"}
}

// TestDialogConfigWritesRunOffLoop pins that the config writes behind
// command palette and settings dialog actions never run on the Update
// goroutine (each is an HTTP round-trip in client/server mode): the action
// returns a command, and the write happens when the command runs.
func TestDialogConfigWritesRunOffLoop(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		action dialog.Action
		key    string
		value  any
	}{
		{"notifications", dialog.ActionSelectNotificationStyle{Style: "bell"}, "options.notifications", "bell"},
		{"composer focus", dialog.ActionToggleComposerFocusOnly{}, "options.tui.composer_focus_only", true},
		{"mouse", dialog.ActionToggleMouseSupport{}, "options.tui.mouse", false},
		{"transparent", dialog.ActionToggleTransparentBackground{}, "options.tui.transparent", true},
		{"theme", dialog.ActionSwitchTheme{Theme: "charmtone-panther"}, "options.tui.active_theme", "charmtone-panther"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			u, ws := newCmdStateUI(t)
			cmd := u.handleDialogAction(tc.action)
			_, written := ws.set(tc.key)
			require.False(t, written, "the write must not run on the Update goroutine")

			msgs := collectMsgs(cmd)
			v, written := ws.set(tc.key)
			require.True(t, written)
			require.Equal(t, tc.value, v)
			_, ok := findMsg[configWriteMsg](msgs)
			require.True(t, ok)
		})
	}
}

// TestToggleAgentCPUsDefersWrite checks the CPU toggle without running its
// command, which would move the test process between CPUs.
func TestToggleAgentCPUsDefersWrite(t *testing.T) {
	t.Parallel()

	u, ws := newCmdStateUI(t)
	require.NotNil(t, u.handleDialogAction(dialog.ActionToggleAgentCPUs{}))
	_, written := ws.set("options.agent_cpus_enabled")
	require.False(t, written)
}

func TestComposerFocusOnlyMovesFocusAfterWrite(t *testing.T) {
	t.Parallel()

	u, _ := newCmdStateUI(t)
	u.focus = uiFocusMain
	msgs := collectMsgs(u.handleDialogAction(dialog.ActionToggleComposerFocusOnly{}))
	require.Equal(t, uiFocusMain, u.focus, "focus waits for the write")

	written, ok := findMsg[configWriteMsg](msgs)
	require.True(t, ok)
	u.Update(written)
	require.Equal(t, uiFocusEditor, u.focus)
}

func TestSwitchThemeWriteFailureKeepsPicker(t *testing.T) {
	t.Parallel()

	u, ws := newCmdStateUI(t)
	ws.setErr = os.ErrPermission
	u.dialog.OpenDialog(idDialog{id: dialog.ThemeID})
	msgs := collectMsgs(u.handleDialogAction(dialog.ActionSwitchTheme{Theme: "charmtone-panther"}))
	written, ok := findMsg[configWriteMsg](msgs)
	require.True(t, ok)
	u.Update(written)
	require.True(t, u.dialog.ContainsDialog(dialog.ThemeID))
	require.False(t, u.theme.userSelected)
}

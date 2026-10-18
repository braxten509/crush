package backend

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/charmbracelet/crush/internal/question"
	"github.com/charmbracelet/crush/internal/secureentry"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// Secure entry has one form per process, so these tests do not run in
// parallel.

// secureTestAgentStart runs piece D's hook for a started interactive agent,
// as a successful interactive InitAgent does.
func secureTestAgentStart(b *Backend, ws *Workspace, clientID string) {
	b.shareHooks().agentInit(ws, clientID, true)
}

// secureTestAsk opens a secure form for sessionID as `crush ask` does, with
// one secure field whose destination is a new file in a folder of its own.
// It returns the form and the destination.
func secureTestAsk(t *testing.T, sessionID string) (*secureentry.Form, string) {
	t.Helper()
	dest := filepath.Join(t.TempDir(), "secret.env")
	require.NoError(t, os.WriteFile(dest, []byte("KEY=%s\n"), 0o600))
	req := question.Request{ID: uuid.NewString(), SessionID: sessionID, Questions: []question.Question{
		{ID: "key", Type: question.TypeSecureEntry, Label: "API key", Text: "Paste the API key"},
	}}
	form, err := secureentry.OpenForm(sessionID, req, map[string]secureentry.Spec{"key": {File: dest, Label: "API key", Occurrence: 1}})
	require.NoError(t, err)
	t.Cleanup(form.Cancel)
	return form, dest
}

// secureTestOpenForms is how many open forms ws holds.
func secureTestOpenForms(ws *Workspace) int {
	ws.share.mu.Lock()
	defer ws.share.mu.Unlock()
	return len(ws.share.forms)
}

// TestSecureEntryWorksInEngine: once the engine's agent started, `crush
// ask` with a secure field opens a form instead of failing with "requires
// the local Crush terminal", the app that views the chat gets it, and its
// submit writes the file.
func TestSecureEntryWorksInEngine(t *testing.T) {
	b, ws := shareTestWorkspace(t, nil)
	viewer, events := shareTestApp(t, b, ws)
	shareTestView(t, b, ws, viewer, "chat-1")
	secureTestAgentStart(b, ws, viewer)

	form, dest := secureTestAsk(t, "chat-1")
	ev := shareTestWaitEvent(t, events, pubsub.PayloadTypeSecureForm)
	sent := ev.Payload.(proto.SecureForm)
	require.Equal(t, "chat-1", sent.SessionID)
	require.Equal(t, []proto.SecureField{{QuestionID: "key", Label: "API key", Status: "open"}}, sent.Fields)

	require.NoError(t, b.SubmitSecureForm(ws.ID, viewer, sent.FormID, nil, map[string][]byte{"key": []byte("v4lue")}))
	result := shareTestRecv(t, form.Result(), 5*time.Second)
	require.False(t, result.Cancelled)
	require.Equal(t, secureentry.StatusSaved, result.Statuses["key"])
	data, err := os.ReadFile(dest)
	require.NoError(t, err)
	require.Equal(t, "KEY=v4lue\n", string(data))

	closed := shareTestWaitEvent(t, events, pubsub.PayloadTypeSecureFormClosed)
	require.Equal(t, proto.SecureFormClosed{FormID: sent.FormID, Reason: "submitted"}, closed.Payload)
	forms, err := b.SecureForms(ws.ID, viewer)
	require.NoError(t, err)
	require.Empty(t, forms)
}

// TestSecureFormDeliveredOnViewChange: a form nobody views waits; the app
// that opens its chat gets it.
func TestSecureFormDeliveredOnViewChange(t *testing.T) {
	b, ws := shareTestWorkspace(t, nil)
	app, events := shareTestApp(t, b, ws)
	secureTestAgentStart(b, ws, app)

	form, _ := secureTestAsk(t, "chat-1")
	require.Eventually(t, func() bool { return secureTestOpenForms(ws) == 1 }, 5*time.Second, 10*time.Millisecond)
	for _, ev := range shareTestDrain(events) {
		require.NotEqual(t, pubsub.PayloadTypeSecureForm, ev.Type, "nobody views the chat")
	}
	forms, err := b.SecureForms(ws.ID, app)
	require.NoError(t, err)
	require.Empty(t, forms)

	shareTestView(t, b, ws, app, "chat-1")
	ev := shareTestWaitEvent(t, events, pubsub.PayloadTypeSecureForm)
	require.Equal(t, "chat-1", ev.Payload.(proto.SecureForm).SessionID)
	forms, err = b.SecureForms(ws.ID, app)
	require.NoError(t, err)
	require.Len(t, forms, 1)
	require.False(t, form.Closed())
}

// TestSecureFormRedeliveredAfterFullStream: a form a full stream dropped
// stays open, is listed, and comes again when the app opens the chat again.
func TestSecureFormRedeliveredAfterFullStream(t *testing.T) {
	b, ws := shareTestWorkspace(t, nil)
	app, events := shareTestApp(t, b, ws)
	shareTestView(t, b, ws, app, "chat-1")
	secureTestAgentStart(b, ws, app)
	shareTestFill(b, ws, app, clientStreamCapacity)

	form, _ := secureTestAsk(t, "chat-1")
	require.Eventually(t, func() bool {
		forms, err := b.SecureForms(ws.ID, app)
		return err == nil && len(forms) == 1
	}, 5*time.Second, 10*time.Millisecond)
	require.False(t, form.Closed())
	for _, ev := range shareTestDrain(events) {
		require.NotEqual(t, pubsub.PayloadTypeSecureForm, ev.Type, "the full stream took the form")
	}

	shareTestView(t, b, ws, app, "chat-2")
	shareTestView(t, b, ws, app, "chat-1")
	ev := shareTestWaitEvent(t, events, pubsub.PayloadTypeSecureForm)
	require.Equal(t, "chat-1", ev.Payload.(proto.SecureForm).SessionID)
}

// TestSecureFieldStatus: the list and the event give each field the status
// secure entry reports for it (secureentry's own TestSecureFieldStatus
// checks those reports).
func TestSecureFieldStatus(t *testing.T) {
	t.Parallel()
	require.Equal(t, []proto.SecureField{
		{QuestionID: "fresh", Label: "Main key", Status: "open"},
		{QuestionID: "written", Label: "API key", Status: "saved"},
		{QuestionID: "left", Label: "Other key", Status: "unavailable"},
	}, secureFieldsProto([]secureentry.FieldInfo{
		{QuestionID: "fresh", Label: "Main key", Available: true},
		{QuestionID: "written", Label: "API key", Saved: true},
		{QuestionID: "left", Label: "Other key"},
	}))
}

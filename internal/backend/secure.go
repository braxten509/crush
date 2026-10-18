package backend

import (
	"errors"
	"maps"
	"slices"
	"sync"

	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/charmbracelet/crush/internal/question"
	"github.com/charmbracelet/crush/internal/secureentry"
	"github.com/google/uuid"
)

// Secure entry through the engine (engine-design.md 5.8). `crush ask` with
// secure fields opens a secureentry.Form. The engine sends it to the apps
// that view its chat, as metadata that names each field by its label only;
// the destination file stays in the engine. A value travels once: from the
// app to the engine in the submit body, which the server takes only on the
// engine's unix socket. The engine writes it to its file.

// Secure entry errors (engine-design.md, section 0.1). Their texts are
// fixed: no error may ever carry a value.
var (
	// ErrSecureFormNotFound is returned for an unknown or closed secure
	// form.
	ErrSecureFormNotFound = errors.New("secure entry not found")
	// ErrNotFormRecipient is returned when an app acts on a secure form
	// that was not sent to it.
	ErrNotFormRecipient = errors.New("this secure entry was not sent to this app")
	// ErrLocalOnly is returned when a secure entry route is reached other
	// than on the engine's unix socket.
	ErrLocalOnly = errors.New("secure entry answers only on the engine's local socket")
)

// The reasons of [proto.SecureFormClosed].
const (
	secureClosedSubmitted = "submitted"
	secureClosedCancelled = "cancelled"
	secureClosedElsewhere = "answered_elsewhere"
	secureClosedClosed    = "closed"
)

// The statuses of [proto.SecureField], as the terminal's secure field shows
// them (ui/dialog/questionform/secure.go).
const (
	secureFieldOpen        = "open"
	secureFieldSaved       = "saved"
	secureFieldUnavailable = "unavailable"
)

// secureForm is a secure form sent to the apps of a workspace.
type secureForm struct {
	id   string
	form *secureentry.Form
	// root is the root chat of the form's session.
	root string
	// recipients are the apps the form was sent to. Guarded by
	// shareState.mu.
	recipients map[string]bool

	// actMu serializes the recipients' submits and cancels, so the form's
	// watcher learns which one closed it.
	actMu sync.Mutex
	// actor is the recipient that closed the form, and action how.
	// Guarded by actMu.
	actor, action string
}

// secureAgentInit connects ws to secure entry at its first interactive
// agent start: in the engine, the apps take the forms the local terminal
// takes in-process.
func (b *Backend) secureAgentInit(ws *Workspace, _ string, interactive bool) {
	if !interactive {
		return
	}
	st := &ws.share
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.closed || st.detach != nil {
		return
	}
	st.detach = secureentry.AttachForms(func(f *secureentry.Form) { b.deliverSecureForm(ws, f) })
}

// StartSecureEntryForTest connects ws to secure entry as its first
// interactive agent start does, for tests in other packages whose
// synthetic workspaces cannot start a real agent.
func StartSecureEntryForTest(b *Backend, ws *Workspace) {
	b.secureAgentInit(ws, "", true)
}

// deliverSecureForm takes a form `crush ask` opened and sends it to every
// app whose current chat has the form's root chat. With no such app, the
// form waits until one opens the chat.
func (b *Backend) deliverSecureForm(ws *Workspace, f *secureentry.Form) {
	sf := &secureForm{
		id:         uuid.NewString(),
		form:       f,
		root:       secureRoot(ws, f.Session),
		recipients: make(map[string]bool),
	}
	st := &ws.share
	st.mu.Lock()
	if st.closed {
		st.mu.Unlock()
		f.Cancel()
		return
	}
	if st.forms == nil {
		st.forms = make(map[string]*secureForm)
	}
	st.forms[sf.id] = sf
	st.mu.Unlock()

	go b.watchSecureForm(ws, sf)
	for clientID, sessionID := range secureViewers(ws) {
		if secureRoot(ws, sessionID) == sf.root {
			b.offerSecureForm(ws, sf, clientID)
		}
	}
}

// secureViewChanged sends the open forms of the chat clientID now shows to
// it, again if it had them: a form a full stream dropped arrives this way.
func (b *Backend) secureViewChanged(ws *Workspace, clientID, sessionID string) {
	if sessionID == "" {
		return
	}
	ws.share.mu.Lock()
	forms := slices.Collect(maps.Values(ws.share.forms))
	ws.share.mu.Unlock()
	if len(forms) == 0 {
		return
	}
	root := secureRoot(ws, sessionID)
	for _, sf := range forms {
		if sf.root == root {
			b.offerSecureForm(ws, sf, clientID)
		}
	}
}

// offerSecureForm makes clientID a recipient of sf and sends it the form. A
// stream that does not take it changes nothing else: the app stays a
// recipient, lists the form after it reconnects, and gets it again when it
// opens the chat again.
func (b *Backend) offerSecureForm(ws *Workspace, sf *secureForm, clientID string) {
	ev := EngineEvent{Type: pubsub.PayloadTypeSecureForm, Kind: pubsub.CreatedEvent, Payload: secureFormProto(sf)}
	st := &ws.share
	st.mu.Lock()
	defer st.mu.Unlock()
	// Under the lock, so the form never follows its own closed event.
	if st.forms[sf.id] != sf {
		return
	}
	sf.recipients[clientID] = true
	b.SendTo(ws.ID, clientID, ev)
}

// watchSecureForm waits for sf to close, then tells each recipient: the one
// that submitted or cancelled it hears which, the others that it was
// answered elsewhere. When no recipient closed it, all hear "closed".
func (b *Backend) watchSecureForm(ws *Workspace, sf *secureForm) {
	<-sf.form.Done()
	// A recipient's submit or cancel still holds actMu while it learns
	// whether it closed the form.
	sf.actMu.Lock()
	actor, action := sf.actor, sf.action
	sf.actMu.Unlock()

	st := &ws.share
	st.mu.Lock()
	defer st.mu.Unlock()
	delete(st.forms, sf.id)
	for clientID := range sf.recipients {
		reason := secureClosedClosed
		switch {
		case actor == "":
		case clientID == actor:
			reason = action
		default:
			reason = secureClosedElsewhere
		}
		b.SendTo(ws.ID, clientID, EngineEvent{
			Type:    pubsub.PayloadTypeSecureFormClosed,
			Kind:    pubsub.DeletedEvent,
			Payload: proto.SecureFormClosed{FormID: sf.id, Reason: reason},
		})
	}
}

// SecureForms lists the open secure forms sent to clientID.
func (b *Backend) SecureForms(workspaceID, clientID string) ([]proto.SecureForm, error) {
	ws, err := b.shareWorkspace(workspaceID, clientID)
	if err != nil {
		return nil, err
	}
	ws.share.mu.Lock()
	var forms []*secureForm
	for _, sf := range ws.share.forms {
		if sf.recipients[clientID] {
			forms = append(forms, sf)
		}
	}
	ws.share.mu.Unlock()
	out := make([]proto.SecureForm, 0, len(forms))
	for _, sf := range forms {
		if !sf.form.Closed() {
			out = append(out, secureFormProto(sf))
		}
	}
	return out, nil
}

// SubmitSecureForm writes values into their files and resolves the form
// with answers. values maps secure question IDs to values; the caller
// clears them afterwards. A *secureentry.FieldError leaves the form open,
// so the app can fix the entry and submit again.
func (b *Backend) SubmitSecureForm(workspaceID, clientID, formID string, answers []question.Answer, values map[string][]byte) error {
	sf, err := b.secureFormFor(workspaceID, clientID, formID)
	if err != nil {
		return err
	}
	sf.actMu.Lock()
	defer sf.actMu.Unlock()
	if sf.form.Closed() {
		return ErrSecureFormNotFound
	}
	sf.actor, sf.action = clientID, secureClosedSubmitted
	if err := sf.form.Submit(answers, values); err != nil {
		sf.actor, sf.action = "", ""
		if errors.Is(err, secureentry.ErrClosed) {
			return ErrSecureFormNotFound
		}
		return err
	}
	return nil
}

// CancelSecureForm closes the form without saving anything more.
func (b *Backend) CancelSecureForm(workspaceID, clientID, formID string) error {
	sf, err := b.secureFormFor(workspaceID, clientID, formID)
	if err != nil {
		return err
	}
	sf.actMu.Lock()
	defer sf.actMu.Unlock()
	if sf.form.Closed() {
		return ErrSecureFormNotFound
	}
	sf.actor, sf.action = clientID, secureClosedCancelled
	sf.form.Cancel()
	return nil
}

// secureFormFor returns the open form formID, which must have been sent to
// clientID.
func (b *Backend) secureFormFor(workspaceID, clientID, formID string) (*secureForm, error) {
	ws, err := b.shareWorkspace(workspaceID, clientID)
	if err != nil {
		return nil, err
	}
	ws.share.mu.Lock()
	defer ws.share.mu.Unlock()
	sf := ws.share.forms[formID]
	if sf == nil {
		return nil, ErrSecureFormNotFound
	}
	if !sf.recipients[clientID] {
		return nil, ErrNotFormRecipient
	}
	return sf, nil
}

// secureTeardown cancels the forms sent to ws's apps and disconnects ws from
// secure entry.
func (b *Backend) secureTeardown(ws *Workspace) {
	st := &ws.share
	st.mu.Lock()
	st.closed = true
	detach := st.detach
	st.detach = nil
	forms := slices.Collect(maps.Values(st.forms))
	st.mu.Unlock()
	for _, sf := range forms {
		sf.form.Cancel()
	}
	if detach != nil {
		detach()
	}
}

// secureFormProto describes sf for an app: the questions as metadata and
// each secure field by its label and status, never by its file.
func secureFormProto(sf *secureForm) proto.SecureForm {
	return proto.SecureForm{
		FormID:    sf.id,
		SessionID: sf.form.Session,
		Request:   proto.QuestionRequestFromDomain(sf.form.Request),
		Fields:    secureFieldsProto(sf.form.Fields()),
	}
}

// secureFieldsProto gives each field the status the terminal shows for it:
// saved, unavailable once its file was saved without it, else open.
func secureFieldsProto(infos []secureentry.FieldInfo) []proto.SecureField {
	fields := make([]proto.SecureField, 0, len(infos))
	for _, f := range infos {
		status := secureFieldOpen
		switch {
		case f.Saved:
			status = secureFieldSaved
		case !f.Available:
			status = secureFieldUnavailable
		}
		fields = append(fields, proto.SecureField{QuestionID: f.QuestionID, Label: f.Label, Status: status})
	}
	return fields
}

// secureViewers returns the chat each registered app of ws shows, for the
// apps that show one.
func secureViewers(ws *Workspace) map[string]string {
	ws.clientsMu.Lock()
	defer ws.clientsMu.Unlock()
	out := make(map[string]string, len(ws.clients))
	for clientID, cs := range ws.clients {
		if cs.currentSessionID != "" {
			out[clientID] = cs.currentSessionID
		}
	}
	return out
}

// secureRoot returns the root chat of sessionID: a helper's chat belongs to
// its parent's form. A failed lookup counts the chat as its own root.
func secureRoot(ws *Workspace, sessionID string) string {
	root, _ := rootSessionID(shareContext(ws), ws, sessionID)
	return root
}

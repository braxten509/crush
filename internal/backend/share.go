package backend

import (
	"cmp"
	"context"
	"errors"
	"log/slog"
	"maps"
	"slices"
	"strconv"
	"sync"
	"time"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/agent"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/google/uuid"
)

// Phone sharing through the engine (engine-design.md 5.7). Each app may run
// one share of the project. The engine runs every phone action that does not
// need the window's own state, with the terminal's checks and texts, and
// forwards only new_chat_send, yolo and mode to the app that started the
// share.
//
// The share itself is a remote.Server. The backend cannot import package
// remote, which imports the backend through package workspace, so it works
// on [ShareServer] and the server package installs the starter
// ([SetShareStarter]).

// Phone sharing errors (engine-design.md, section 0.1).
var (
	// ErrNotShareOwner is returned when an app acts on a phone share that
	// another app started.
	ErrNotShareOwner = errors.New("another app started this phone share")
	// ErrShareOff is returned when no app runs a phone share.
	ErrShareOff = errors.New("this app runs no phone share")
	// ErrActionNotFound is returned when no phone action waits under the
	// given ID.
	ErrActionNotFound = errors.New("no phone action waits under that ID")
)

// The phone's answers, word for word as the terminal and the share give them
// (ui/model/remote.go, remote/remote.go, ui/model/model_select.go and
// ui/model/fast_mode.go).
var (
	errPhoneOtherChat          = errors.New("the Crush window switched to another chat")
	errPhoneAgentNotReady      = errors.New("the agent is not ready yet")
	errPhoneAgentBusy          = errors.New("Agent is busy, please wait...") //nolint:staticcheck
	errPhoneNoBackground       = errors.New("no running command can move to the background")
	errPhoneFastNeedsCodex     = errors.New("FAST mode requires a Codex model") //nolint:staticcheck
	errPhoneNothingToSummarize = errors.New("a new chat has nothing to summarize yet")
	errPhoneNoCLI              = errors.New("that CLI is not set up on the computer")
	errPhoneNoModel            = errors.New("that model is not available on the computer")
	errPhoneNoConfig           = errors.New("configuration not found")
	errPhoneNoAgentConfig      = errors.New("agent configuration not found")
	errPhoneNoThinkToggle      = errors.New("selected model does not support a thinking toggle")
	errPhoneNoFastMode         = errors.New("selected model does not support FAST mode")
	errPhoneNoUltracode        = errors.New("selected model does not support Ultracode")
	errPhoneNotResponding      = errors.New("the Crush window is not responding")
	errPhoneWindowNoAnswer     = errors.New("the Crush window did not answer")
	errPhoneEngineNoAnswer     = errors.New("the Crush engine did not answer")
	errPhoneWindowClosed       = errors.New("the Crush window closed")
	errPhoneShareOff           = errors.New("Remote Control is off") //nolint:staticcheck
)

// shareReplyTimeout is how long a phone action may take before the engine
// answers for it. It is under the share's own 30 s wait (remote/remote.go),
// so the phone hears the engine's answer, not the share's 504.
const shareReplyTimeout = 25 * time.Second

// The states of [proto.RemoteStatus].
const (
	remoteStateOff   = "off"
	remoteStateOn    = "on"
	remoteStateSetup = "setup"
)

// The kinds of [proto.RemoteAction]: the phone actions the window runs.
const (
	remoteKindNewChatSend = "new_chat_send"
	remoteKindYolo        = "yolo"
	remoteKindMode        = "mode"
)

// ShareActionKind is what the phone asks for. The values are those of
// remote.ActionKind, in the same order, so "unknown request <n>" names the
// phone's own number.
type ShareActionKind int

const (
	ShareSend       ShareActionKind = iota // send Text and Attachments
	ShareCancel                            // the terminal treats it as interrupt
	ShareInterrupt                         // stop and send the queue now
	ShareBackground                        // move the running command to the background
	ShareClearQueue                        // drop the queued prompts
	ShareModel                             // switch to Provider/Model
	ShareEffort                            // set the reasoning Effort
	ShareThink                             // toggle thinking
	ShareFast                              // toggle FAST mode
	ShareUltracode                         // toggle Claude's Ultracode
	ShareYolo                              // toggle YOLO mode (the window runs it)
	ShareMode                              // switch code and plan mode (the window runs it)
	ShareSummarize                         // /compact
)

// ShareAction is one phone request (remote.Action). Done reports its
// outcome to the phone; the engine calls it exactly once.
type ShareAction struct {
	Kind        ShareActionKind
	SessionID   string // the chat the phone saw; "" for the window's new chat
	Text        string
	Attachments []message.Attachment
	Provider    string
	Model       string
	Effort      string
	Done        func(error)
}

// ShareStatus describes a running share (remote.Status).
type ShareStatus struct {
	Host   string
	Port   int
	Phones []string
}

// ShareServer is one running phone share (remote.Server).
type ShareServer interface {
	// Actions delivers the phone's requests.
	Actions() <-chan *ShareAction
	// PhonesChanged fires when a phone connects or disconnects.
	PhonesChanged() <-chan struct{}
	// Stopped is closed once the share stops.
	Stopped() <-chan struct{}
	// SetPresence tells the phone what the owner's window shows.
	SetPresence(proto.RemotePresence)
	// SetLaunch changes the launch ID the share reports to the launcher.
	SetLaunch(id string)
	Status() ShareStatus
	// Stop ends the share and disconnects every phone.
	Stop()
}

// ShareSetupError reports a Tailscale setup step that keeps a share from
// starting (remote.SetupError).
type ShareSetupError struct {
	Setup proto.RemoteSetup
}

func (e *ShareSetupError) Error() string { return e.Setup.Title + ". " + e.Setup.Detail }

// ShareStarter starts a phone share of ws. launchID is the window's
// CRUSH_REMOTE_LAUNCH value, if any; the share reports it, so the launcher
// finds the window. A setup step the share needs first comes back as a
// [*ShareSetupError].
type ShareStarter func(ctx context.Context, ws *Workspace, launchID string) (ShareServer, error)

// shareStarter is the starter [SetShareStarter] installed.
var shareStarter struct {
	sync.Mutex
	start ShareStarter
}

// SetShareStarter installs the function that starts phone shares. The
// server package installs one built on remote.StartShare.
func SetShareStarter(start ShareStarter) {
	shareStarter.Lock()
	shareStarter.start = start
	shareStarter.Unlock()
}

// SetShareStarterForTest makes ws start its phone shares with start instead
// of the installed starter, so tests can run shares without Tailscale.
func SetShareStarterForTest(ws *Workspace, start ShareStarter) {
	ws.share.mu.Lock()
	ws.share.start = start
	ws.share.mu.Unlock()
}

// errShareUnavailable is a start's error when no starter is installed.
var errShareUnavailable = errors.New("phone sharing is not available in this Crush")

// shareState is piece D's state per workspace: the apps' phone shares (5.7)
// and the secure forms sent to its apps (5.8).
type shareState struct {
	mu sync.Mutex
	// shares holds the running shares by owner client ID; one per app.
	shares map[string]*share
	// forms holds the open secure forms by form ID (5.8).
	forms map[string]*secureForm
	// detach disconnects the workspace from secure entry (5.8). It is set
	// at the first interactive agent start.
	detach func()
	// closed is set at teardown. No share starts and no form is taken
	// after it.
	closed bool

	// startMu keeps one app from starting two shares at once.
	startMu sync.Mutex

	// Test seams: start replaces the installed starter, timeout replaces
	// shareReplyTimeout and send replaces [Backend.SendMessage].
	start   ShareStarter
	timeout time.Duration
	send    func(workspaceID, clientID string, msg proto.AgentMessage) error
}

// share is one app's running phone share.
type share struct {
	owner string
	srv   ShareServer
	// pending holds the actions forwarded to the owner's window that wait
	// for its done, by action ID. Guarded by shareState.mu.
	pending map[string]*pendingAction
	// ended is set once the share was removed. Guarded by shareState.mu.
	ended bool
	// quit is closed when the share ends, which stops its pump.
	quit chan struct{}
}

// pendingAction is a phone action forwarded to the share owner's window.
type pendingAction struct {
	kind  string // the RemoteAction kind
	reply func(error)
	timer *time.Timer
	// text and attachments are new_chat_send's prompt. The engine keeps
	// them until the window has created the chat, and drops them when the
	// action ends.
	text        string
	attachments []message.Attachment
}

// take hands over the kept prompt and drops it from the action.
func (pa *pendingAction) take() (string, []message.Attachment) {
	text, atts := pa.text, pa.attachments
	pa.text, pa.attachments = "", nil
	return text, atts
}

// fail answers the phone with err and drops the kept prompt.
func (pa *pendingAction) fail(err error) {
	pa.take()
	pa.reply(err)
}

// StartRemote starts clientID's phone share, or returns its running share's
// status: one share per app. A repeat start with a launch ID makes the
// running share report that ID, so the launcher finds the window again; an
// empty one changes nothing. A setup step answers state "setup" with its
// info; any other failure answers state "off" with the error.
func (b *Backend) StartRemote(ctx context.Context, workspaceID, clientID, launchID string) (proto.RemoteStatus, error) {
	ws, err := b.shareWorkspace(workspaceID, clientID)
	if err != nil {
		return proto.RemoteStatus{}, err
	}
	st := &ws.share
	st.startMu.Lock()
	defer st.startMu.Unlock()

	st.mu.Lock()
	closed, running, start := st.closed, st.shares[clientID], st.start
	st.mu.Unlock()
	switch {
	case closed:
		return proto.RemoteStatus{}, ErrWorkspaceClosing
	case running != nil:
		if launchID != "" {
			running.srv.SetLaunch(launchID)
		}
		return running.status(), nil
	}
	if start == nil {
		shareStarter.Lock()
		start = shareStarter.start
		shareStarter.Unlock()
	}
	if start == nil {
		return proto.RemoteStatus{State: remoteStateOff, Error: errShareUnavailable.Error()}, nil
	}
	srv, err := start(ctx, ws, launchID)
	if err != nil {
		var setup *ShareSetupError
		if errors.As(err, &setup) {
			info := setup.Setup
			return proto.RemoteStatus{State: remoteStateSetup, Setup: &info}, nil
		}
		return proto.RemoteStatus{State: remoteStateOff, Error: err.Error()}, nil
	}

	sh := &share{owner: clientID, srv: srv, pending: make(map[string]*pendingAction), quit: make(chan struct{})}
	st.mu.Lock()
	// The app may have left, or the workspace closed, while the share
	// started; its gone and teardown hooks found nothing to stop then.
	if st.closed || !shareClientRegistered(ws, clientID) {
		closed := st.closed
		st.mu.Unlock()
		srv.Stop()
		if closed {
			return proto.RemoteStatus{}, ErrWorkspaceClosing
		}
		return proto.RemoteStatus{}, ErrClientNotAttached
	}
	if st.shares == nil {
		st.shares = make(map[string]*share)
	}
	st.shares[clientID] = sh
	st.mu.Unlock()
	go b.pumpShare(ws, sh)
	return sh.status(), nil
}

// StopRemote stops clientID's phone share.
func (b *Backend) StopRemote(workspaceID, clientID string) error {
	ws, err := b.shareWorkspace(workspaceID, clientID)
	if err != nil {
		return err
	}
	sh, err := shareOwnedBy(ws, clientID)
	if err != nil {
		return err
	}
	b.endShare(ws, sh, errPhoneShareOff)
	return nil
}

// RemoteStatus describes clientID's phone share; state "off" when it runs
// none.
func (b *Backend) RemoteStatus(workspaceID, clientID string) (proto.RemoteStatus, error) {
	ws, err := b.shareWorkspace(workspaceID, clientID)
	if err != nil {
		return proto.RemoteStatus{}, err
	}
	ws.share.mu.Lock()
	sh := ws.share.shares[clientID]
	ws.share.mu.Unlock()
	if sh == nil {
		return proto.RemoteStatus{State: remoteStateOff}, nil
	}
	return sh.status(), nil
}

// SetRemotePresence tells clientID's phone share what the window shows. The
// share follows the chat of the app that started it.
func (b *Backend) SetRemotePresence(workspaceID, clientID string, p proto.RemotePresence) error {
	ws, err := b.shareWorkspace(workspaceID, clientID)
	if err != nil {
		return err
	}
	sh, err := shareOwnedBy(ws, clientID)
	if err != nil {
		return err
	}
	sh.srv.SetPresence(p)
	return nil
}

// FinishRemoteAction takes the window's outcome of a forwarded phone action.
// An empty Error means done. For new_chat_send, the window has created its
// new chat and shows it; the engine then sends the kept prompt there, and
// the phone hears that send's outcome.
func (b *Backend) FinishRemoteAction(workspaceID, clientID, actionID string, done proto.RemoteActionDone) error {
	ws, err := b.shareWorkspace(workspaceID, clientID)
	if err != nil {
		return err
	}
	sh, pa, err := takeFinishedAction(ws, clientID, actionID)
	if err != nil {
		return err
	}
	pa.timer.Stop()
	switch {
	case done.Error != "":
		pa.fail(errors.New(done.Error))
	case pa.kind != remoteKindNewChatSend:
		pa.reply(nil)
	default:
		text, atts := pa.take()
		if current, _ := b.ClientCurrentSession(ws.ID, sh.owner); done.SessionID == "" || done.SessionID != current {
			pa.reply(errPhoneOtherChat)
			return nil
		}
		pa.reply(b.shareSend(ws, sh.owner, done.SessionID, text, atts))
	}
	return nil
}

// takeFinishedAction removes actionID from the share of clientID that waits
// for it.
func takeFinishedAction(ws *Workspace, clientID, actionID string) (*share, *pendingAction, error) {
	st := &ws.share
	st.mu.Lock()
	defer st.mu.Unlock()
	for _, sh := range st.shares {
		pa := sh.pending[actionID]
		if pa == nil {
			continue
		}
		if sh.owner != clientID {
			return nil, nil, ErrNotShareOwner
		}
		delete(sh.pending, actionID)
		return sh, pa, nil
	}
	switch {
	case st.shares[clientID] != nil:
		return nil, nil, ErrActionNotFound
	case len(st.shares) > 0:
		return nil, nil, ErrNotShareOwner
	default:
		return nil, nil, ErrShareOff
	}
}

// shareWorkspace finds the workspace and checks that clientID is one of its
// apps.
func (b *Backend) shareWorkspace(workspaceID, clientID string) (*Workspace, error) {
	if _, err := validateClientID(clientID); err != nil {
		return nil, err
	}
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}
	if !shareClientRegistered(ws, clientID) {
		return nil, ErrClientNotAttached
	}
	return ws, nil
}

// shareClientRegistered reports whether clientID holds a claim on ws:
// attached, or inside its grace.
func shareClientRegistered(ws *Workspace, clientID string) bool {
	ws.clientsMu.Lock()
	defer ws.clientsMu.Unlock()
	_, ok := ws.clients[clientID]
	return ok
}

// shareOwnedBy returns clientID's running share. Without one it reports
// ErrNotShareOwner while another app shares, else ErrShareOff.
func shareOwnedBy(ws *Workspace, clientID string) (*share, error) {
	st := &ws.share
	st.mu.Lock()
	defer st.mu.Unlock()
	if sh := st.shares[clientID]; sh != nil {
		return sh, nil
	}
	if len(st.shares) > 0 {
		return nil, ErrNotShareOwner
	}
	return nil, ErrShareOff
}

// status describes the running share.
func (sh *share) status() proto.RemoteStatus {
	s := sh.srv.Status()
	return proto.RemoteStatus{State: remoteStateOn, Host: s.Host, Port: s.Port, Phones: s.Phones}
}

// shareTimeout is how long ws's phone actions may take.
func shareTimeout(ws *Workspace) time.Duration {
	ws.share.mu.Lock()
	defer ws.share.mu.Unlock()
	return cmp.Or(ws.share.timeout, shareReplyTimeout)
}

// pumpShare takes each phone action from the share at once and never waits
// on an app or on the work: the share answers 503 when an action is not
// taken within 5 s. It also tells the owner when phones change, and ends
// the share when it stops.
func (b *Backend) pumpShare(ws *Workspace, sh *share) {
	for {
		select {
		case a, ok := <-sh.srv.Actions():
			if !ok {
				b.endShare(ws, sh, errPhoneShareOff)
				return
			}
			b.takeShareAction(ws, sh, a)
		case <-sh.srv.PhonesChanged():
			status := sh.status()
			// Under the lock, so this never follows the "off" that
			// endShare sends.
			ws.share.mu.Lock()
			if !sh.ended {
				b.SendTo(ws.ID, sh.owner, EngineEvent{Type: pubsub.PayloadTypeRemoteStatus, Kind: pubsub.UpdatedEvent, Payload: status})
			}
			ws.share.mu.Unlock()
		case <-sh.srv.Stopped():
			b.endShare(ws, sh, errPhoneShareOff)
			return
		case <-sh.quit:
			return
		}
	}
}

// takeShareAction starts a phone action without waiting for it. Every
// action gets exactly one answer within the share's reply timeout.
func (b *Backend) takeShareAction(ws *Workspace, sh *share, a *ShareAction) {
	var once sync.Once
	reply := func(err error) {
		once.Do(func() {
			if a.Done != nil {
				a.Done(err)
			}
		})
	}
	timeout := shareTimeout(ws)
	if kind, ok := shareWindowKind(a); ok {
		b.forwardShareAction(ws, sh, a, kind, timeout, reply)
		return
	}
	go func() {
		result := make(chan error, 1)
		go func() { result <- b.runShareAction(ws, sh.owner, a) }()
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		select {
		case err := <-result:
			reply(err)
		case <-timer.C:
			reply(errPhoneEngineNoAnswer)
			err := <-result
			slog.Warn("A phone action ended after the engine answered for it", "kind", int(a.Kind), "error", err)
		}
	}()
}

// shareWindowKind reports whether only the owner's window can run a, with
// its RemoteAction kind: a send to the window's new chat, YOLO and the input
// mode change the window's own state.
func shareWindowKind(a *ShareAction) (string, bool) {
	switch {
	case a.Kind == ShareSend && a.SessionID == "":
		return remoteKindNewChatSend, true
	case a.Kind == ShareYolo:
		return remoteKindYolo, true
	case a.Kind == ShareMode:
		return remoteKindMode, true
	}
	return "", false
}

// forwardShareAction hands a to the owner's window as a remote_action event
// and waits for its done, up to timeout. An event no stream took fails at
// once. A new_chat_send event carries only references to its uploads: the
// bytes stay here.
func (b *Backend) forwardShareAction(ws *Workspace, sh *share, a *ShareAction, kind string, timeout time.Duration, reply func(error)) {
	id := uuid.NewString()
	ev := proto.RemoteAction{ActionID: id, Kind: kind, SessionID: a.SessionID}
	pa := &pendingAction{kind: kind, reply: reply}
	if kind == remoteKindNewChatSend {
		ev.Text = a.Text
		for _, att := range a.Attachments {
			ev.Uploads = append(ev.Uploads, proto.RemoteUpload{
				UploadID: uuid.NewString(),
				Name:     att.FileName,
				Mime:     att.MimeType,
				Size:     len(att.Content),
			})
		}
		pa.text, pa.attachments = a.Text, a.Attachments
	}

	st := &ws.share
	st.mu.Lock()
	if sh.ended {
		st.mu.Unlock()
		pa.fail(errPhoneShareOff)
		return
	}
	pa.timer = time.AfterFunc(timeout, func() {
		if pa := takePendingAction(ws, sh, id); pa != nil {
			pa.fail(errPhoneWindowNoAnswer)
		}
	})
	sh.pending[id] = pa
	st.mu.Unlock()

	if b.SendTo(ws.ID, sh.owner, EngineEvent{Type: pubsub.PayloadTypeRemoteAction, Kind: pubsub.CreatedEvent, Payload: ev}) == 0 {
		// No stream took it, so no window will answer.
		if pa := takePendingAction(ws, sh, id); pa != nil {
			pa.timer.Stop()
			pa.fail(errPhoneNotResponding)
		}
	}
}

// takePendingAction removes the forwarded action id from sh, or returns nil
// when it already ended.
func takePendingAction(ws *Workspace, sh *share, id string) *pendingAction {
	ws.share.mu.Lock()
	defer ws.share.mu.Unlock()
	pa := sh.pending[id]
	delete(sh.pending, id)
	return pa
}

// endShare removes sh, fails its waiting actions with reason, stops the
// share and tells the owner it is off. It does nothing once sh ended.
func (b *Backend) endShare(ws *Workspace, sh *share, reason error) {
	st := &ws.share
	st.mu.Lock()
	if sh.ended {
		st.mu.Unlock()
		return
	}
	sh.ended = true
	if st.shares[sh.owner] == sh {
		delete(st.shares, sh.owner)
	}
	pending := sh.pending
	sh.pending = nil
	close(sh.quit)
	st.mu.Unlock()

	for _, pa := range pending {
		pa.timer.Stop()
		pa.fail(reason)
	}
	sh.srv.Stop()
	b.SendTo(ws.ID, sh.owner, EngineEvent{Type: pubsub.PayloadTypeRemoteStatus, Kind: pubsub.UpdatedEvent, Payload: proto.RemoteStatus{State: remoteStateOff}})
}

// runShareAction runs a phone action in the engine with the terminal's
// checks and texts (ui/model/remote.go runRemoteAction), on behalf of the
// share owner.
func (b *Backend) runShareAction(ws *Workspace, owner string, a *ShareAction) error {
	current, _ := b.ClientCurrentSession(ws.ID, owner)
	if a.SessionID != current {
		return errPhoneOtherChat
	}
	coord := shareCoordinator(ws)
	switch a.Kind {
	case ShareSend:
		if coord == nil {
			return errPhoneAgentNotReady
		}
		return b.shareSend(ws, owner, current, a.Text, a.Attachments)
	case ShareCancel, ShareInterrupt:
		// The terminal's cancel is its interrupt, which does nothing in a
		// new chat or before the agent is ready (ui/model/send.go).
		if current == "" || coord == nil {
			return nil
		}
		return b.Interrupt(ws.ID, current)
	case ShareBackground:
		if current == "" {
			return errPhoneNoBackground
		}
		moved, err := b.MoveToBackground(ws.ID, current)
		if err != nil {
			return err
		}
		if !moved {
			return errPhoneNoBackground
		}
		return nil
	case ShareClearQueue:
		if current == "" {
			return nil
		}
		return b.ClearQueue(ws.ID, current)
	}

	// The rest change the model or summarize, which the terminal refuses
	// while the agent works. The engine cannot see a window's own "!"
	// command or a prompt it is still submitting.
	if coord != nil && coord.IsBusy() {
		return errPhoneAgentBusy
	}
	switch a.Kind {
	case ShareModel:
		return b.shareSelectModel(ws, a.Provider, a.Model)
	case ShareEffort:
		return b.shareSetEffort(ws, a.Effort)
	case ShareThink:
		return b.shareToggleThinking(ws)
	case ShareFast:
		if p := shareCoderProvider(ws); p == nil || p.Type != config.TypeCodexCLI {
			return errPhoneFastNeedsCodex
		}
		return b.shareToggleFast(ws)
	case ShareUltracode:
		return b.shareToggleUltracode(ws)
	case ShareSummarize:
		if current == "" {
			return errPhoneNothingToSummarize
		}
		return b.shareSummarize(ws, current)
	}
	return errors.New("unknown request " + strconv.Itoa(int(a.Kind)))
}

// shareSend sends the phone's prompt to sessionID with a new submission ID
// and the owner's client ID, so the engine counts it as the owner's prompt
// (5.5).
func (b *Backend) shareSend(ws *Workspace, owner, sessionID, text string, atts []message.Attachment) error {
	msg := proto.AgentMessage{
		SubmissionID: uuid.NewString(),
		SessionID:    sessionID,
		Prompt:       text,
		Attachments:  proto.AttachmentsFromMessage(atts),
	}
	send := b.SendMessage
	ws.share.mu.Lock()
	if ws.share.send != nil {
		send = ws.share.send
	}
	ws.share.mu.Unlock()
	if err := send(ws.ID, owner, msg); err != nil {
		if errors.Is(err, ErrAgentNotInitialized) {
			return errPhoneAgentNotReady
		}
		return err
	}
	return nil
}

// shareSummarize starts summarizing sessionID and returns at once, as the
// terminal does (ui/model/send.go summarizeSession). The work is bound to
// the workspace, which waits for it at teardown.
func (b *Backend) shareSummarize(ws *Workspace, sessionID string) error {
	ws.runMu.Lock()
	if ws.closing {
		ws.runMu.Unlock()
		return ErrWorkspaceClosing
	}
	ws.runWG.Add(1)
	ws.runMu.Unlock()
	go func() {
		defer ws.runWG.Done()
		if err := b.SummarizeSession(shareContext(ws), ws.ID, sessionID); err != nil && !errors.Is(err, context.Canceled) {
			slog.Error("Summarizing a chat for the phone failed", "error", err)
		}
	}()
	return nil
}

// shareSelectModel switches the coder to a model of a set-up provider, as
// picking it in the terminal's models dialog does (ui/model/remote.go
// remoteSelectModel, then ui/model/model_select.go selectModel).
func (b *Backend) shareSelectModel(ws *Workspace, providerID, modelID string) error {
	cfg := shareConfig(ws)
	if cfg == nil {
		return errPhoneNoConfig
	}
	pc, ok := cfg.Providers.Get(providerID)
	if !ok || pc.Disable {
		return errPhoneNoCLI
	}
	models := pc.AvailableModels()
	var model *catwalk.Model
	for i := range models {
		if models[i].ID == modelID {
			model = &models[i]
			break
		}
	}
	if model == nil {
		return errPhoneNoModel
	}
	// Here the terminal would open a sign-in dialog in the window. The
	// engine has none, so a provider still needing a sign-in is not set up.
	if !b.shareProviderSignedIn(ws, pc) {
		return errPhoneNoCLI
	}
	selected := config.SelectedModel{
		Model:           model.ID,
		Provider:        pc.ID,
		ReasoningEffort: model.DefaultReasoningEffort,
		MaxTokens:       model.DefaultMaxTokens,
	}
	kind := cmp.Or(cfg.Agents[config.AgentCoder].Model, config.SelectedModelTypeLarge)
	if err := b.UpdatePreferredModel(ws.ID, config.ScopeGlobal, kind, selected); err != nil {
		return err
	}
	// As in the terminal, a missing small model is filled in too.
	if _, hasSmall := cfg.Models[config.SelectedModelTypeSmall]; !hasSmall && kind != config.SelectedModelTypeSmall {
		small, err := b.GetDefaultSmallModel(ws.ID, selected.Provider)
		if err == nil {
			err = b.UpdatePreferredModel(ws.ID, config.ScopeGlobal, config.SelectedModelTypeSmall, small)
		}
		if err != nil {
			slog.Warn("Could not set the small model after a phone model change", "error", err)
		}
	}
	return b.shareUpdateAgent(ws)
}

// shareProviderSignedIn reports whether pc has the credential the terminal
// would otherwise ask for in a dialog (ui/model/model_select.go): a fresh
// Hyper token, refreshed silently as there, and an account login or an API
// key for OpenAI and xAI.
func (b *Backend) shareProviderSignedIn(ws *Workspace, pc config.ProviderConfig) bool {
	switch pc.ID {
	case "hyper":
		if pc.OAuthToken == nil || !pc.OAuthToken.IsExpired() {
			return true
		}
		ctx, cancel := context.WithTimeout(shareContext(ws), 15*time.Second)
		defer cancel()
		if err := b.RefreshOAuthToken(ctx, ws.ID, config.ScopeGlobal, pc.ID); err != nil {
			slog.Warn("Hyper OAuth refresh for a phone model change failed", "error", err)
			return false
		}
		return true
	case string(catwalk.InferenceProviderOpenAI), string(catwalk.InferenceProviderXAI):
		return pc.OAuthToken != nil || (ws.Cfg != nil && pc.HasAPIKey(ws.Cfg.Resolver()))
	}
	return true
}

// shareSetEffort stores the reasoning effort for the coder's model
// (ui/model/model_select.go setReasoningEffort). The phone hears the save's
// outcome; a failed model rebuild is only logged, as in the terminal.
func (b *Backend) shareSetEffort(ws *Workspace, effort string) error {
	cfg, kind, err := shareCoderConfig(ws)
	if err != nil {
		return err
	}
	current := cfg.Models[kind]
	if err := cfg.ValidateReasoningEffort(current.Provider, current.Model, effort); err != nil {
		return err
	}
	current.ReasoningEffort = effort
	if err := b.UpdatePreferredModel(ws.ID, config.ScopeGlobal, kind, current); err != nil {
		return err
	}
	if err := b.shareUpdateAgent(ws); err != nil {
		slog.Warn("Could not rebuild the agent after a phone effort change", "error", err)
	}
	return nil
}

// shareToggleThinking flips thinking for a model without reasoning levels
// (ui/model/model_select.go toggleThinking). A failed model rebuild is only
// logged, as in the terminal.
func (b *Backend) shareToggleThinking(ws *Workspace) error {
	cfg, kind, err := shareCoderConfig(ws)
	if err != nil {
		return err
	}
	provider, catalog := cfg.GetProviderForModel(kind), cfg.GetModelByType(kind)
	if provider == nil || catalog == nil || !config.SupportsThinkingToggle(*provider, *catalog) {
		return errPhoneNoThinkToggle
	}
	current := cfg.Models[kind]
	current.Think = !current.Think
	if err := b.UpdatePreferredModel(ws.ID, config.ScopeGlobal, kind, current); err != nil {
		return err
	}
	if err := b.shareUpdateAgent(ws); err != nil {
		slog.Warn("Could not rebuild the agent after a phone thinking change", "error", err)
	}
	return nil
}

// shareToggleFast flips FAST mode (ui/model/fast_mode.go toggleFastMode).
func (b *Backend) shareToggleFast(ws *Workspace) error {
	cfg, kind, err := shareCoderConfig(ws)
	if err != nil {
		return err
	}
	provider, model := cfg.GetProviderForModel(kind), cfg.GetModelByType(kind)
	if provider == nil || model == nil || !config.SupportsFastMode(*provider, *model) {
		return errPhoneNoFastMode
	}
	selected := cfg.Models[kind]
	if selected.ServiceTier == "fast" {
		selected.ServiceTier = "default"
	} else {
		selected.ServiceTier = "fast"
	}
	if err := b.UpdatePreferredModel(ws.ID, config.ScopeGlobal, kind, selected); err != nil {
		return err
	}
	return b.shareUpdateAgent(ws)
}

// shareToggleUltracode flips Claude's Ultracode (ui/model/fast_mode.go
// toggleUltracode).
func (b *Backend) shareToggleUltracode(ws *Workspace) error {
	cfg, kind, err := shareCoderConfig(ws)
	if err != nil {
		return err
	}
	provider, model := cfg.GetProviderForModel(kind), cfg.GetModelByType(kind)
	if provider == nil || model == nil || !config.SupportsUltracode(*provider, *model) {
		return errPhoneNoUltracode
	}
	selected := cfg.Models[kind]
	selected.Ultracode = !selected.Ultracode
	if err := b.UpdatePreferredModel(ws.ID, config.ScopeGlobal, kind, selected); err != nil {
		return err
	}
	return b.shareUpdateAgent(ws)
}

// shareUpdateAgent rebuilds the agent's models after a preference change.
// Before the agent started there is nothing to rebuild: it reads the saved
// preference when it starts.
func (b *Backend) shareUpdateAgent(ws *Workspace) error {
	if shareCoordinator(ws) == nil {
		return nil
	}
	return b.UpdateAgent(shareContext(ws), ws.ID)
}

// shareConfig returns ws's configuration, or nil when it has none.
func shareConfig(ws *Workspace) *config.Config {
	if ws.Cfg == nil {
		return nil
	}
	return ws.Cfg.Config()
}

// shareCoderConfig returns ws's configuration and the coder's model type,
// with the terminal's texts when either is missing.
func shareCoderConfig(ws *Workspace) (*config.Config, config.SelectedModelType, error) {
	cfg := shareConfig(ws)
	if cfg == nil {
		return nil, "", errPhoneNoConfig
	}
	agentCfg, ok := cfg.Agents[config.AgentCoder]
	if !ok {
		return nil, "", errPhoneNoAgentConfig
	}
	return cfg, agentCfg.Model, nil
}

// shareCoderProvider returns the provider of the coder's model, or nil
// (ui/model/remote.go coderProvider).
func shareCoderProvider(ws *Workspace) *config.ProviderConfig {
	cfg, kind, err := shareCoderConfig(ws)
	if err != nil {
		return nil
	}
	return cfg.GetProviderForModel(kind)
}

// shareCoordinator returns ws's agent coordinator, or nil before the agent
// started.
func shareCoordinator(ws *Workspace) agent.Coordinator {
	if ws.App == nil {
		return nil
	}
	return ws.AgentCoordinator
}

// shareContext is the workspace's run context.
func shareContext(ws *Workspace) context.Context {
	if ws.ctx == nil {
		return context.Background()
	}
	return ws.ctx
}

// shareHooks are piece D's client hooks: an app's share ends when it leaves
// (5.7), and secure entry follows the apps' agent starts and chats (5.8).
func (b *Backend) shareHooks() clientHooks {
	return clientHooks{
		viewChanged: b.secureViewChanged,
		agentInit:   b.secureAgentInit,
		gone: func(ws *Workspace, clientID string, _ GoneReason, _ bool) {
			ws.share.mu.Lock()
			sh := ws.share.shares[clientID]
			ws.share.mu.Unlock()
			if sh != nil {
				b.endShare(ws, sh, errPhoneWindowClosed)
			}
		},
		teardown: b.shareTeardown,
	}
}

// shareTeardown stops every share of ws and closes its secure entry.
func (b *Backend) shareTeardown(ws *Workspace) {
	ws.share.mu.Lock()
	ws.share.closed = true
	shares := slices.Collect(maps.Values(ws.share.shares))
	ws.share.mu.Unlock()
	for _, sh := range shares {
		b.endShare(ws, sh, errPhoneWindowClosed)
	}
	b.secureTeardown(ws)
}

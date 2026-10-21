package server

import (
	"context"
	"errors"
	"net/http"
	"net/netip"

	"github.com/charmbracelet/crush/internal/backend"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/remote"
	"github.com/charmbracelet/crush/internal/rpc"
	"github.com/charmbracelet/crush/internal/workspace"
)

// Phone sharing through the engine (engine-design.md 5.7): the routes, and
// the share starter the backend runs. The backend cannot import package
// remote, which imports the backend through package workspace, so the
// server installs the starter and adapts each remote.Server to
// backend.ShareServer.

func init() { backend.SetShareStarter(startEngineShare) }

// startRemote starts the phone share of the chat the calling app shows.
func (c *controllerV1) startRemote(r *http.Request, req proto.RemoteStart) (proto.RemoteStatus, error) {
	clientID, err := c.clientID(r)
	if err != nil {
		return proto.RemoteStatus{}, err
	}
	return c.backend.StartRemote(r.Context(), r.PathValue("id"), clientID, req.SessionID, req.LaunchID)
}

// stopRemote stops a chat's phone share.
func (c *controllerV1) stopRemote(r *http.Request, req proto.RemoteStop) (rpc.Empty, error) {
	clientID, err := c.clientID(r)
	if err != nil {
		return rpc.Empty{}, err
	}
	return rpc.Empty{}, c.backend.StopRemote(r.PathValue("id"), clientID, req.SessionID)
}

// getRemoteStatus describes the phone shares the calling app sees.
func (c *controllerV1) getRemoteStatus(r *http.Request, _ rpc.Empty) ([]proto.RemoteStatus, error) {
	clientID, err := c.clientID(r)
	if err != nil {
		return nil, err
	}
	return c.backend.RemoteShares(r.PathValue("id"), clientID)
}

// setRemotePresence takes what the calling app's window shows.
func (c *controllerV1) setRemotePresence(r *http.Request, p proto.RemotePresence) (rpc.Empty, error) {
	clientID, err := c.clientID(r)
	if err != nil {
		return rpc.Empty{}, err
	}
	return rpc.Empty{}, c.backend.SetRemotePresence(r.PathValue("id"), clientID, p)
}

// finishRemoteAction takes the window's outcome of a forwarded phone action.
func (c *controllerV1) finishRemoteAction(r *http.Request, done proto.RemoteActionDone) (rpc.Empty, error) {
	clientID, err := c.clientID(r)
	if err != nil {
		return rpc.Empty{}, err
	}
	return rpc.Empty{}, c.backend.FinishRemoteAction(r.PathValue("id"), clientID, r.PathValue("aid"), done)
}

// startEngineShare starts a phone share of ws with remote.StartShare.
func startEngineShare(ctx context.Context, ws *backend.Workspace, launchID string) (backend.ShareServer, error) {
	srv, err := remote.StartShare(ctx, engineShareSource(ws), launchID)
	if err != nil {
		var setup *remote.SetupError
		if errors.As(err, &setup) {
			return nil, &backend.ShareSetupError{Setup: remoteSetupProto(setup.Info)}
		}
		return nil, err
	}
	return newEngineShare(srv), nil
}

// engineShareSource is what a share of ws reads and answers through: the
// engine's own app, as a terminal's share uses its in-process one. Secure
// entry is not part of it, so no form reaches a phone.
func engineShareSource(ws *backend.Workspace) remote.Source {
	return remote.NewSource(workspace.NewAppWorkspace(ws.App, ws.Cfg))
}

// remoteSetupProto describes a setup step as the terminal's setup dialog
// shows it; the dialog offers the main button only with an action.
func remoteSetupProto(info remote.SetupInfo) proto.RemoteSetup {
	setup := proto.RemoteSetup{State: string(info.State), Title: info.Title, Detail: info.Detail, Action: info.Action}
	if info.Action != "" {
		setup.Button = info.Button
	}
	return setup
}

// remoteShareServer is what the engine uses of a *remote.Server.
type remoteShareServer interface {
	Actions() <-chan *remote.Action
	PhonesChanged() <-chan struct{}
	Stopped() <-chan struct{}
	SetPresence(remote.Presence)
	SetLaunch(string)
	Status() remote.Status
	Stop()
}

// engineShare adapts a remote share to backend.ShareServer.
type engineShare struct {
	srv     remoteShareServer
	actions chan *backend.ShareAction
}

// newEngineShare adapts srv and starts passing its phone actions on.
func newEngineShare(srv remoteShareServer) *engineShare {
	s := &engineShare{srv: srv, actions: make(chan *backend.ShareAction)}
	go s.forward()
	return s
}

// forward hands each phone action to the engine as it arrives, until the
// share stops.
func (s *engineShare) forward() {
	for {
		select {
		case a := <-s.srv.Actions():
			select {
			case s.actions <- shareActionFromRemote(a):
			case <-s.srv.Stopped():
				a.Done(errors.New("Remote Control is off")) //nolint:staticcheck
				return
			}
		case <-s.srv.Stopped():
			return
		}
	}
}

func (s *engineShare) Actions() <-chan *backend.ShareAction { return s.actions }
func (s *engineShare) PhonesChanged() <-chan struct{}       { return s.srv.PhonesChanged() }
func (s *engineShare) Stopped() <-chan struct{}             { return s.srv.Stopped() }
func (s *engineShare) Stop()                                { s.srv.Stop() }
func (s *engineShare) SetLaunch(id string)                  { s.srv.SetLaunch(id) }

func (s *engineShare) SetPresence(p proto.RemotePresence) {
	s.srv.SetPresence(remote.Presence{SessionID: p.SessionID, Focused: p.Focused, Plan: p.Plan})
}

func (s *engineShare) Status() backend.ShareStatus {
	st := s.srv.Status()
	out := backend.ShareStatus{Host: st.Host, Phones: st.Phones}
	if ap, err := netip.ParseAddrPort(st.Addr); err == nil {
		out.Port = int(ap.Port())
	}
	return out
}

// shareKinds maps the phone's action kinds to the engine's.
var shareKinds = map[remote.ActionKind]backend.ShareActionKind{
	remote.ActSend:       backend.ShareSend,
	remote.ActCancel:     backend.ShareCancel,
	remote.ActInterrupt:  backend.ShareInterrupt,
	remote.ActBackground: backend.ShareBackground,
	remote.ActClearQueue: backend.ShareClearQueue,
	remote.ActModel:      backend.ShareModel,
	remote.ActEffort:     backend.ShareEffort,
	remote.ActThink:      backend.ShareThink,
	remote.ActFast:       backend.ShareFast,
	remote.ActUltracode:  backend.ShareUltracode,
	remote.ActYolo:       backend.ShareYolo,
	remote.ActMode:       backend.ShareMode,
	remote.ActSummarize:  backend.ShareSummarize,
}

// shareActionFromRemote is a's request for the engine. Its Done answers
// the phone.
func shareActionFromRemote(a *remote.Action) *backend.ShareAction {
	kind, ok := shareKinds[a.Kind]
	if !ok {
		// An unknown kind keeps its number, which the phone hears back.
		kind = backend.ShareActionKind(a.Kind)
	}
	return &backend.ShareAction{
		Kind:        kind,
		SessionID:   a.SessionID,
		Text:        a.Text,
		Attachments: a.Attachments,
		Provider:    a.Provider,
		Model:       a.Model,
		Effort:      a.Effort,
		Done:        a.Done,
	}
}

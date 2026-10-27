package server

import (
	"encoding/json"
	"fmt"
	"net/http"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/backend"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/rpc"
	"github.com/google/uuid"
)

type controllerV1 struct {
	backend *backend.Backend
	server  *Server
}

// getHealth checks server health.
func (c *controllerV1) getHealth(*http.Request, rpc.Empty) (rpc.Empty, error) {
	return rpc.Empty{}, nil
}

// getVersion returns server version information.
func (c *controllerV1) getVersion(*http.Request, rpc.Empty) (proto.VersionInfo, error) {
	return c.backend.VersionInfo(), nil
}

// postControl runs a server control command.
func (c *controllerV1) postControl(_ *http.Request, req proto.ServerControl) (rpc.Empty, error) {
	switch req.Command {
	case proto.ServerControlShutdown, proto.ServerControlShutdownIfIdle:
		// Both spellings are conditional. Only the backend can rule on
		// idleness without racing a session that arrives between a
		// client's own check and its request, and guarding the plain
		// command too means clients predating the check cannot take live
		// sessions down either.
		if !c.backend.ShutdownIfIdle() {
			return rpc.Empty{}, backend.ErrServerNotIdle
		}
		return rpc.Empty{}, nil
	case proto.ServerControlStop:
		// Explicit, unlike the commands above: the desktop sends it only
		// after its user chose "Stop engine" over the running work.
		c.backend.StopEngine()
		return rpc.Empty{}, nil
	default:
		return rpc.Empty{}, backend.ErrUnknownCommand
	}
}

// getConfig returns the server-level configuration.
func (c *controllerV1) getConfig(*http.Request, rpc.Empty) (*config.ConfigStore, error) {
	return c.backend.Config(), nil
}

// deleteClient retires a client, releasing every claim it holds. With
// keep_work=1 a workspace it was the last app on stays while its work runs
// (the desktop moving to another folder); with keep_work=alone it stays so
// even with no app open (the desktop's last window closing). Older engines
// read keep_work=alone as a plain retire.
func (c *controllerV1) deleteClient(r *http.Request, _ rpc.Empty) (rpc.Empty, error) {
	switch r.URL.Query().Get("keep_work") {
	case "1":
		return rpc.Empty{}, c.backend.RetireClientKeepWork(r.PathValue("client_id"))
	case "alone":
		return rpc.Empty{}, c.backend.RetireClientKeepWorkAlone(r.PathValue("client_id"))
	}
	return rpc.Empty{}, c.backend.RetireClient(r.PathValue("client_id"))
}

// listWorkspaces lists all workspaces.
func (c *controllerV1) listWorkspaces(*http.Request, rpc.Empty) ([]proto.Workspace, error) {
	return c.backend.ListWorkspaces(), nil
}

// getWorkspace returns a single workspace by ID.
func (c *controllerV1) getWorkspace(r *http.Request, _ rpc.Empty) (proto.Workspace, error) {
	return c.backend.GetWorkspaceProto(r.PathValue("id"))
}

// createWorkspace creates a workspace, or joins the one already open at
// the same path.
func (c *controllerV1) createWorkspace(_ *http.Request, args proto.Workspace) (proto.Workspace, error) {
	_, result, err := c.backend.CreateWorkspace(args)
	return result, err
}

// clientID reads the client_id query parameter and validates it as a
// UUID.
func (c *controllerV1) clientID(r *http.Request) (string, error) {
	cid := r.URL.Query().Get("client_id")
	if cid == "" {
		return "", badRequest("client_id is required")
	}
	if _, err := uuid.Parse(cid); err != nil {
		return "", badRequest("client_id is not a valid UUID")
	}
	return cid, nil
}

// optionalClientID reads the optional client_id query parameter: "" when
// it is absent, and a 400 error when it is not a valid UUID.
func (c *controllerV1) optionalClientID(r *http.Request) (string, error) {
	if r.URL.Query().Get("client_id") == "" {
		return "", nil
	}
	return c.clientID(r)
}

// setCurrentSession records the calling client's current session
// selection for the workspace. An empty session_id clears the entry
// (e.g. the client is on the landing screen).
func (c *controllerV1) setCurrentSession(r *http.Request, req proto.CurrentSession) (rpc.Empty, error) {
	clientID, err := c.clientID(r)
	if err != nil {
		return rpc.Empty{}, err
	}
	return rpc.Empty{}, c.backend.SetCurrentSession(r.Context(), r.PathValue("id"), clientID, req.SessionID)
}

// deleteWorkspace releases the calling client's creation hold on a
// workspace.
func (c *controllerV1) deleteWorkspace(r *http.Request, _ rpc.Empty) (rpc.Empty, error) {
	clientID, err := c.clientID(r)
	if err != nil {
		return rpc.Empty{}, err
	}
	return rpc.Empty{}, c.backend.DeleteWorkspace(r.PathValue("id"), clientID)
}

// getWorkspaceConfig returns the workspace configuration.
func (c *controllerV1) getWorkspaceConfig(r *http.Request, _ rpc.Empty) (*config.Config, error) {
	return c.backend.GetWorkspaceConfig(r.PathValue("id"))
}

// getWorkspaceProviders lists the providers available to a workspace.
func (c *controllerV1) getWorkspaceProviders(r *http.Request, _ rpc.Empty) ([]catwalk.Provider, error) {
	return c.backend.GetWorkspaceProviders(r.PathValue("id"))
}

// handleGetWorkspaceEvents streams workspace events as Server-Sent Events.
func (c *controllerV1) handleGetWorkspaceEvents(w http.ResponseWriter, r *http.Request) {
	flusher := http.NewResponseController(w)
	id := r.PathValue("id")
	clientID, err := c.clientID(r)
	if err != nil {
		c.handleError(w, r, err)
		return
	}
	// Subscribe to the event broker BEFORE attaching the client.
	// AttachClient bumps the stream count that observers use to
	// detect a live subscriber; subscribing first guarantees that
	// once a client appears attached, any published event is
	// delivered rather than dropped on a not-yet-registered stream.
	events, err := c.backend.SubscribeEvents(r.Context(), id)
	if err != nil {
		c.handleError(w, r, err)
		return
	}
	// The engine's own events for this stream, subscribed before the
	// attach for the same reason.
	engineEvents, err := c.backend.SubscribeClient(r.Context(), id, clientID)
	if err != nil {
		c.handleError(w, r, err)
		return
	}
	// watch=1 is a listener that holds nothing (owner, 2026-10-20: "only
	// ring ONCE - from the desktop one"): the desktop hears the folders of
	// open chats no window shows, and the workspace and engine still close
	// when their own clients go.
	if r.URL.Query().Get("watch") != "1" {
		if err := c.backend.AttachClient(id, clientID); err != nil {
			c.handleError(w, r, err)
			return
		}
		defer c.backend.DetachClient(id, clientID)
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	// Flush headers immediately so clients see the 200 response
	// before any events arrive. Without this, a quiet workspace
	// keeps the client's SubscribeEvents call blocked on the
	// initial RoundTrip.
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	for {
		var payload any
		select {
		case <-r.Context().Done():
			c.server.logDebug(r, "Stopping event stream")
			return
		case ev, ok := <-events:
			if !ok {
				return
			}
			if !c.deliverToClient(r.Context(), id, clientID, ev.Payload) {
				continue
			}
			payload = ev.Payload
		case ev, ok := <-engineEvents:
			if !ok {
				// It closes when the request ends, which can win the
				// select over the Done case above.
				c.server.logDebug(r, "Stopping event stream")
				return
			}
			// Engine events are already addressed, so they skip
			// deliverToClient.
			payload = ev
		}
		wrapped := wrapEvent(payload)
		if wrapped == nil {
			continue
		}
		data, err := json.Marshal(wrapped)
		if err != nil {
			c.server.logError(r, "Failed to marshal event", "error", err)
			continue
		}

		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}
}

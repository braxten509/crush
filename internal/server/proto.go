package server

// This file held the v1 handlers before they moved into the handlers_*.go
// files around the typed rpc descriptors. It now keeps the handlers of the
// endpoints upstream v0.100.0 added for host applications (host context and
// session notes) and for permission modes; the host theme, pinentry and SSH
// handlers live in theme.go, pinentry.go and ssh.go.

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/rpc"
)

// getHostContext returns what the host application has in view.
func (c *controllerV1) getHostContext(r *http.Request, _ rpc.Empty) (proto.HostContext, error) {
	return c.backend.GetHostContext(r.PathValue("id"))
}

// handlePostWorkspaceHostContext records what the host application has in
// view. An empty body clears it, so the body is decoded by hand: a typed
// route only skips decoding when the request announces no length.
func (c *controllerV1) handlePostWorkspaceHostContext(w http.ResponseWriter, r *http.Request) {
	var hc proto.HostContext
	if err := json.NewDecoder(r.Body).Decode(&hc); err != nil && !errors.Is(err, io.EOF) {
		c.server.logError(r, "Failed to decode request", "error", err)
		jsonError(w, http.StatusBadRequest, "failed to decode request")
		return
	}
	if err := c.backend.SetHostContext(r.PathValue("id"), hc); err != nil {
		c.handleError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// addSessionNote keeps a note for the session's next prompt.
func (c *controllerV1) addSessionNote(r *http.Request, note proto.SessionNote) (rpc.Empty, error) {
	if note.Text == "" {
		return rpc.Empty{}, badRequest("failed to decode request")
	}
	return rpc.Empty{}, c.backend.AddNote(r.PathValue("id"), r.PathValue("sid"), note.Text)
}

// setPermissionMode sets the permission mode for a workspace.
func (c *controllerV1) setPermissionMode(r *http.Request, req proto.PermissionSetModeRequest) (rpc.Empty, error) {
	return rpc.Empty{}, c.backend.SetPermissionMode(r.PathValue("id"), proto.ProtoModeToPermission(req.Mode))
}

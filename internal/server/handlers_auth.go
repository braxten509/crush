package server

import (
	"encoding/json"
	"net/http"

	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/rpc"
)

// Sign-in routes (desktop design 5-6, 2.4; G-5). Each answers 403
// local_only before it reads anything unless it came in on the engine's
// unix socket, and none logs its body.

// verifyKeyMaxBody caps a key check's body.
const verifyKeyMaxBody = 64 << 10

// localOnly refuses a request that did not come in on the unix socket
// before next reads it.
func (c *controllerV1) localOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := secureLocalOnly(r); err != nil {
			c.handleError(w, r, err)
			return
		}
		next(w, r)
	}
}

// startAuthFlow starts a platform's sign-in.
func (c *controllerV1) startAuthFlow(r *http.Request, req proto.AuthFlowRequest) (proto.AuthFlow, error) {
	if req.Platform == "" {
		return proto.AuthFlow{}, badRequest("platform is required")
	}
	return c.backend.StartAuthFlow(r.PathValue("id"), req.Platform)
}

// sendAuthCode finishes a sign-in with a pasted code.
func (c *controllerV1) sendAuthCode(r *http.Request, req proto.AuthCodeRequest) (rpc.Empty, error) {
	if req.Code == "" {
		return rpc.Empty{}, badRequest("code is required")
	}
	return rpc.Empty{}, c.backend.SendAuthCode(r.PathValue("flow_id"), req.Code)
}

// cancelAuthFlow stops a sign-in.
func (c *controllerV1) cancelAuthFlow(r *http.Request, _ rpc.Empty) (rpc.Empty, error) {
	return rpc.Empty{}, c.backend.CancelAuthFlow(r.PathValue("flow_id"))
}

// handleVerifyKey tests an API key without saving it. It decodes the body
// itself so that nothing of it, not even a decode error, is logged.
func (c *controllerV1) handleVerifyKey(w http.ResponseWriter, r *http.Request) {
	var req proto.VerifyKeyRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, verifyKeyMaxBody)).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "could not read the key check")
		return
	}
	if req.ProviderID == "" || req.APIKey == "" {
		jsonError(w, http.StatusBadRequest, "provider_id and api_key are required")
		return
	}
	resp, err := c.backend.VerifyAPIKey(r.PathValue("id"), req.ProviderID, req.APIKey)
	req.APIKey = ""
	if err != nil {
		c.handleError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

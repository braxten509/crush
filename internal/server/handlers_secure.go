package server

import (
	"encoding/json"
	"net"
	"net/http"

	"github.com/charmbracelet/crush/internal/backend"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/question"
	"github.com/charmbracelet/crush/internal/rpc"
)

// Secure entry through the engine (engine-design.md 5.8). The three routes
// answer only on the engine's unix socket, which only its owner can open:
// the legacy server started with a TCP --host can never take a value over
// the network. Each checks the listener before anything else.

// secureSubmitMaxBody caps a secure entry's request body.
const secureSubmitMaxBody = 1 << 20

// secureLocalOnly refuses a request that did not come in on a unix socket.
func secureLocalOnly(r *http.Request) error {
	if _, ok := r.Context().Value(http.LocalAddrContextKey).(*net.UnixAddr); ok {
		return nil
	}
	return backend.ErrLocalOnly
}

// listSecureForms lists the open secure forms sent to the calling app.
func (c *controllerV1) listSecureForms(r *http.Request, _ rpc.Empty) ([]proto.SecureForm, error) {
	if err := secureLocalOnly(r); err != nil {
		return nil, err
	}
	clientID, err := c.clientID(r)
	if err != nil {
		return nil, err
	}
	return c.backend.SecureForms(r.PathValue("id"), clientID)
}

// cancelSecureForm closes a secure form without saving anything more.
func (c *controllerV1) cancelSecureForm(r *http.Request, _ rpc.Empty) (rpc.Empty, error) {
	if err := secureLocalOnly(r); err != nil {
		return rpc.Empty{}, err
	}
	clientID, err := c.clientID(r)
	if err != nil {
		return rpc.Empty{}, err
	}
	return rpc.Empty{}, c.backend.CancelSecureForm(r.PathValue("id"), clientID, r.PathValue("fid"))
}

// handleSubmitSecureForm answers a secure form. It is hand-written so that
// no part of the body reaches a log: a body that does not decode gets fixed
// text and is not logged (serve logs decode errors, and a JSON error can
// quote a character of the body), every error text is fixed, and the
// values are zeroed once the engine wrote them.
func (c *controllerV1) handleSubmitSecureForm(w http.ResponseWriter, r *http.Request) {
	if err := secureLocalOnly(r); err != nil {
		c.handleError(w, r, err)
		return
	}
	clientID, err := c.clientID(r)
	if err != nil {
		c.handleError(w, r, err)
		return
	}
	var req proto.SecureSubmit
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, secureSubmitMaxBody)).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "could not read the secure entry")
		return
	}
	values := make(map[string][]byte, len(req.Values))
	for id, value := range req.Values {
		values[id] = []byte(value)
	}
	req.Values = nil
	var answers []question.Answer
	for _, answer := range req.Answers {
		answers = append(answers, proto.QuestionAnswerToDomain(answer)...)
	}
	err = c.backend.SubmitSecureForm(r.PathValue("id"), clientID, r.PathValue("fid"), answers, values)
	for _, value := range values {
		clear(value)
	}
	if err != nil {
		c.handleError(w, r, err)
		return
	}
	w.WriteHeader(rpc.SubmitSecureForm.SuccessStatus())
}

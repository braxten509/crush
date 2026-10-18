package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/charmbracelet/crush/internal/apigen"
	"github.com/charmbracelet/crush/internal/backend"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/rpc"
)

// opHandler is the typed body of an endpoint: it receives the request
// (for path and query values and its context) and the decoded JSON body,
// and returns the response to encode or an error to map to a status.
type opHandler[Req, Resp any] func(r *http.Request, req Req) (Resp, error)

// serveConfig holds the per-endpoint decoding tweaks a few routes need.
type serveConfig struct {
	optionalBody bool
	maxBody      int64
	decodeError  string
}

// serveOption adjusts how [serve] decodes a request.
type serveOption func(*serveConfig)

// optionalBody decodes the request body only when one was sent.
func optionalBody() serveOption {
	return func(c *serveConfig) { c.optionalBody = true }
}

// limitBody caps the request body at n bytes and reports undecodable
// bodies with msg.
func limitBody(n int64, msg string) serveOption {
	return func(c *serveConfig) {
		c.maxBody = n
		c.decodeError = msg
	}
}

// decodeErrorMessage reports undecodable bodies with msg.
func decodeErrorMessage(msg string) serveOption {
	return func(c *serveConfig) { c.decodeError = msg }
}

// route binds a typed handler to its descriptor and starts the
// endpoint's documentation. Method, path, status, and body schemas all
// come from op; the chain is finished with Build.
func route[Req, Resp any](c *controllerV1, op rpc.Op[Req, Resp], fn opHandler[Req, Resp], opts ...serveOption) apigen.Builder {
	return apigen.For(op).Bind(serve(c, op, fn, opts...))
}

// serve adapts a typed handler to net/http: it decodes the JSON request
// body (when the op has one), runs fn, maps errors through
// [controllerV1.handleError], and writes the op's success status with
// the JSON response (when the op has one).
func serve[Req, Resp any](c *controllerV1, op rpc.Op[Req, Resp], fn opHandler[Req, Resp], opts ...serveOption) http.HandlerFunc {
	cfg := serveConfig{decodeError: "failed to decode request"}
	for _, opt := range opts {
		opt(&cfg)
	}
	return func(w http.ResponseWriter, r *http.Request) {
		var req Req
		if op.HasRequestBody() && (!cfg.optionalBody || (r.Body != nil && r.ContentLength > 0)) {
			var body io.Reader = r.Body
			if cfg.maxBody > 0 {
				body = http.MaxBytesReader(w, r.Body, cfg.maxBody)
			}
			if err := json.NewDecoder(body).Decode(&req); err != nil {
				c.server.logError(r, "Failed to decode request", "error", err)
				jsonError(w, http.StatusBadRequest, cfg.decodeError)
				return
			}
		}
		resp, err := fn(r, req)
		if err != nil {
			c.handleError(w, r, err)
			return
		}
		if !op.HasResponseBody() {
			w.WriteHeader(op.SuccessStatus())
			return
		}
		writeJSON(w, op.SuccessStatus(), resp)
	}
}

// badRequestError is a request validation failure. It is reported as
// 400 with its message.
type badRequestError struct{ msg string }

func (e badRequestError) Error() string { return e.msg }

// badRequest returns a validation error reported as 400 with msg.
func badRequest(msg string) error { return badRequestError{msg: msg} }

// handleError maps backend errors to HTTP status codes and writes the
// JSON error response.
//
// Runtime cancellation of an agent run no longer reaches here for the
// agent-prompt path: SendMessage is fire-and-forget (the handler returns
// 202 before the run starts) and Backend.runAgent swallows
// context.Canceled, surfacing the FinishReasonCanceled marker to SSE
// subscribers instead. The remaining callers pass synchronous backend
// errors, so context.Canceled gets no special case and would fall through
// to the default 500 like any other unexpected error.
func (c *controllerV1) handleError(w http.ResponseWriter, r *http.Request, err error) {
	c.server.logError(r, err.Error())
	status, code, field := errorInfo(err)
	writeError(w, status, proto.Error{Message: err.Error(), Code: code, Field: field})
}

// errorInfo returns the HTTP status, the stable code and the field of the
// error body for a handler error. The build pieces' errors are checked
// first.
func errorInfo(err error) (status int, code, field string) {
	for _, e := range pieceErrors() {
		if e.Match(err) {
			if e.Field != nil {
				field = e.Field(err)
			}
			return e.Status, e.Code, field
		}
	}
	var bad badRequestError
	switch {
	case errors.As(err, &bad):
		return http.StatusBadRequest, "bad_request", ""
	case errors.Is(err, backend.ErrWorkspaceNotFound),
		errors.Is(err, backend.ErrLSPClientNotFound):
		return http.StatusNotFound, "not_found", ""
	case errors.Is(err, backend.ErrAgentNotInitialized),
		errors.Is(err, backend.ErrPathRequired),
		errors.Is(err, backend.ErrInvalidPermissionAction),
		errors.Is(err, backend.ErrUnknownCommand),
		errors.Is(err, backend.ErrInvalidClientID):
		return http.StatusBadRequest, "bad_request", ""
	case errors.Is(err, backend.ErrAgentBusy):
		// Switching the main agent mid-run could strand the run's
		// queued prompts on the previous agent; mirror the TUI's own
		// busy guard for API callers.
		return http.StatusConflict, "agent_busy", ""
	case errors.Is(err, backend.ErrClientNotAttached):
		// 409, not 404: the workspace exists, the caller just has no
		// live stream yet. A 404 here is indistinguishable from
		// "workspace gone" and would trip clients that treat 404 as
		// the trigger for workspace recovery.
		return http.StatusConflict, "client_not_attached", ""
	case errors.Is(err, backend.ErrWorkspaceClosing):
		return http.StatusConflict, "workspace_closing", ""
	case errors.Is(err, backend.ErrServerNotIdle):
		return http.StatusConflict, "server_not_idle", ""
	case errors.Is(err, backend.ErrClientRetired):
		return http.StatusConflict, "client_retired", ""
	case errors.Is(err, backend.ErrServerShuttingDown):
		// 503, not 409: the request is not wrong, this process is just
		// leaving. Clients retry against its replacement.
		return http.StatusServiceUnavailable, "shutting_down", ""
	default:
		return http.StatusInternalServerError, "internal", ""
	}
}

// jsonEncode writes v as a 200 JSON response.
func jsonEncode(w http.ResponseWriter, v any) {
	writeJSON(w, http.StatusOK, v)
}

// writeJSON writes v as a JSON response with the given status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	if status != http.StatusOK {
		w.WriteHeader(status)
	}
	_ = json.NewEncoder(w).Encode(v)
}

// jsonError writes message as a JSON error response, with the code that
// goes with status.
func jsonError(w http.ResponseWriter, status int, message string) {
	writeError(w, status, proto.Error{Message: message, Code: statusCode(status)})
}

// writeError writes body as a JSON error response with the given status.
func writeError(w http.ResponseWriter, status int, body proto.Error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

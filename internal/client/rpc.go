package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/charmbracelet/crush/internal/rpc"
)

// call is one request against a typed [rpc.Op]. Build it with [newCall],
// add path arguments and query values, then run it with [call.do].
type call[Req, Resp any] struct {
	c     *Client
	op    rpc.Op[Req, Resp]
	args  []string
	query url.Values
	// action and noun phrase the call's errors: "failed to <action>"
	// and, for an undecodable response, "failed to decode <noun>".
	action, noun string
}

// newCall starts a call to op. action and noun phrase its errors, e.g.
// "list MCP prompts" and "MCP prompts".
func newCall[Req, Resp any](c *Client, op rpc.Op[Req, Resp], action, noun string) call[Req, Resp] {
	return call[Req, Resp]{c: c, op: op, action: action, noun: noun}
}

// at fills the route's path parameters, in order.
func (k call[Req, Resp]) at(args ...string) call[Req, Resp] {
	k.args = args
	return k
}

// with adds query parameters.
func (k call[Req, Resp]) with(query url.Values) call[Req, Resp] {
	k.query = query
	return k
}

// do sends body as JSON (unless the op takes none), maps any status
// other than 200 or the op's own success status through checkStatus, and
// decodes the response (unless the op returns none; an empty body
// decodes to the zero value). Errors are prefixed with the call's action
// so they read like the client's historical messages.
func (k call[Req, Resp]) do(ctx context.Context, body Req) (Resp, error) {
	var zero Resp
	var (
		reader  io.Reader
		headers http.Header
	)
	if k.op.HasRequestBody() {
		reader = jsonBody(body)
		headers = http.Header{"Content-Type": []string{"application/json"}}
	}
	path := strings.TrimPrefix(k.op.URL(k.args...), apiPrefix)
	rsp, err := k.c.sendReq(ctx, k.op.Method, path, k.query, reader, headers)
	if err != nil {
		return zero, fmt.Errorf("failed to %s: %w", k.action, err)
	}
	defer rsp.Body.Close()
	if err := checkStatus(rsp, http.StatusOK, k.op.SuccessStatus()); err != nil {
		return zero, &refusedError{err: fmt.Errorf("failed to %s: %w", k.action, err)}
	}
	if !k.op.HasResponseBody() {
		return zero, nil
	}
	var out Resp
	if err := json.NewDecoder(rsp.Body).Decode(&out); err != nil && !errors.Is(err, io.EOF) {
		return zero, fmt.Errorf("failed to decode %s: %w", k.noun, err)
	}
	return out, nil
}

// get runs a call that sends no body.
func (k call[Req, Resp]) get(ctx context.Context) (Resp, error) {
	var none Req
	return k.do(ctx, none)
}

// refusedError marks a call the server answered with an error status, as
// opposed to one that failed in transit (where the server may still have
// acted on the request).
type refusedError struct{ err error }

func (e *refusedError) Error() string { return e.err.Error() }
func (e *refusedError) Unwrap() error { return e.err }

// isRefused reports whether err is a server's refusal of the request.
func isRefused(err error) bool {
	var refused *refusedError
	return errors.As(err, &refused)
}

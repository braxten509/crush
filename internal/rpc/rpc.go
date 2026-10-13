// Package rpc declares the Crush v1 HTTP API as typed descriptors. Each
// [Op] names one endpoint's method and route pattern and, through its
// type parameters, the JSON bodies it accepts and returns. The server
// registers its handlers, the client issues its calls, and the API docs
// are generated from the same descriptors, so the three cannot drift: a
// mismatch between client and server is a compile error.
package rpc

import (
	"net/http"
	"strings"
)

// Empty marks an [Op] without a request body or without a response
// body. An Empty response is a bare success status.
type Empty struct{}

// Op describes one endpoint of the API. Req is the JSON request body
// and Resp the JSON response body; either may be [Empty].
type Op[Req, Resp any] struct {
	// Method is the HTTP method, e.g. "GET".
	Method string
	// Path is the route pattern in net/http.ServeMux syntax, including
	// the /v1 prefix, e.g. "/v1/workspaces/{id}/sessions/{sid}".
	Path string
	// Status is the success status code. Zero means 200.
	Status int
}

// URL fills the route pattern's {name} segments, in order, with args.
// Values are inserted verbatim, matching how the client has always built
// its paths. Surplus arguments are ignored; missing ones leave the
// segment empty.
func (o Op[Req, Resp]) URL(args ...string) string {
	var b strings.Builder
	rest := o.Path
	for {
		start := strings.IndexByte(rest, '{')
		if start < 0 {
			b.WriteString(rest)
			return b.String()
		}
		end := strings.IndexByte(rest[start:], '}')
		if end < 0 {
			b.WriteString(rest)
			return b.String()
		}
		b.WriteString(rest[:start])
		if len(args) > 0 {
			b.WriteString(args[0])
			args = args[1:]
		}
		rest = rest[start+end+1:]
	}
}

// SuccessStatus returns the success status code, defaulting to 200.
func (o Op[Req, Resp]) SuccessStatus() int {
	if o.Status == 0 {
		return http.StatusOK
	}
	return o.Status
}

// RouteMethod returns the HTTP method. It, together with the other
// Route* methods, lets the docs generator build an endpoint from an Op.
func (o Op[Req, Resp]) RouteMethod() string { return o.Method }

// RoutePath returns the route pattern.
func (o Op[Req, Resp]) RoutePath() string { return o.Path }

// RouteStatus returns the success status code.
func (o Op[Req, Resp]) RouteStatus() int { return o.SuccessStatus() }

// RequestExample returns a zero request body for schema generation, or
// nil when the endpoint takes no body.
func (o Op[Req, Resp]) RequestExample() any { return example[Req]() }

// ResponseExample returns a zero response body for schema generation,
// or nil when the endpoint returns no body.
func (o Op[Req, Resp]) ResponseExample() any { return example[Resp]() }

// HasRequestBody reports whether the endpoint takes a JSON body.
func (o Op[Req, Resp]) HasRequestBody() bool { return !isEmpty[Req]() }

// HasResponseBody reports whether the endpoint returns a JSON body.
func (o Op[Req, Resp]) HasResponseBody() bool { return !isEmpty[Resp]() }

func isEmpty[T any]() bool {
	var zero T
	_, ok := any(zero).(Empty)
	return ok
}

func example[T any]() any {
	if isEmpty[T]() {
		return nil
	}
	var zero T
	return zero
}

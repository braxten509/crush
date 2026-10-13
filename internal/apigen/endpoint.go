// Package apigen provides a fluent API for documenting HTTP endpoints.
//
// An [Endpoint] is the single source of truth for one route: the same
// value drives mux registration and documentation generation, so the
// two cannot drift. Endpoints are constructed with the method builders
// ([Get], [Post], [Put], [Delete]) or from a typed [Route] with [For],
// and finalized with [Builder.Handle] (or [Builder.Build] for a builder
// whose handler was bound up front with [Builder.Bind]). A chain without
// its finalizer does not compile into a []Endpoint, so incomplete
// definitions fail at build time.
package apigen

import "net/http"

// Param documents a single path or query parameter.
type Param struct {
	// Name is the parameter name as it appears in the path or query.
	Name string
	// In is "path" or "query".
	In string
	// Type is the JSON schema type, e.g. "string", "integer".
	Type string
	// Description is a short human-readable description.
	Description string
	// Required reports whether the parameter must be provided. Path
	// parameters are always required.
	Required bool
}

// Endpoint is the documentation and routing record for a single HTTP
// endpoint. Construct it with the fluent builders ([Get], [Post], ...,
// [For]); only [Builder.Handle] and [Builder.Build] produce one.
type Endpoint struct {
	method      string
	path        string
	summary     string
	description string
	tags        []string
	params      []Param
	request     any
	response    any
	status      int
	errors      []int
	sse         bool
	handler     http.HandlerFunc
}

// Method returns the HTTP method, e.g. "GET".
func (e Endpoint) Method() string { return e.method }

// Path returns the route pattern, e.g. "/v1/workspaces/{id}".
func (e Endpoint) Path() string { return e.path }

// Summary returns the one-line summary.
func (e Endpoint) Summary() string { return e.summary }

// Description returns the optional longer prose description.
func (e Endpoint) Description() string { return e.description }

// Tags returns the grouping tags.
func (e Endpoint) Tags() []string { return e.tags }

// Params returns the documented path and query parameters.
func (e Endpoint) Params() []Param { return e.params }

// Request returns the request body example value, or nil for none.
func (e Endpoint) Request() any { return e.request }

// Response returns the success response example value, or nil for an
// empty success response.
func (e Endpoint) Response() any { return e.response }

// Status returns the success status code, defaulting to 200.
func (e Endpoint) Status() int {
	if e.status == 0 {
		return http.StatusOK
	}
	return e.status
}

// Errors returns the status codes that return an error object.
func (e Endpoint) Errors() []int { return e.errors }

// SSE reports whether the endpoint streams text/event-stream instead
// of producing a JSON response.
func (e Endpoint) SSE() bool { return e.sse }

// Handler returns the endpoint's HTTP handler.
func (e Endpoint) Handler() http.HandlerFunc { return e.handler }

// Builder accumulates endpoint documentation fluently. It is
// deliberately a distinct type from [Endpoint]: a []Endpoint literal
// cannot contain a builder, so a chain missing Handle fails to
// compile.
type Builder struct{ e Endpoint }

// Route is a typed endpoint descriptor (see internal/rpc). It supplies
// the method and path pattern, the success status, and zero values of
// the request and response bodies, whose types drive the schemas. A nil
// example means the endpoint has no such body.
type Route interface {
	RouteMethod() string
	RoutePath() string
	RouteStatus() int
	RequestExample() any
	ResponseExample() any
}

// For begins an endpoint described by a typed route: its method, path,
// status, and request and response schemas come from r, so the route
// table and the docs cannot disagree about them.
func For(r Route) Builder {
	return Builder{Endpoint{
		method:   r.RouteMethod(),
		path:     r.RoutePath(),
		request:  r.RequestExample(),
		response: r.ResponseExample(),
		status:   r.RouteStatus(),
	}}
}

// Get begins a GET endpoint for path, e.g. Get("/v1/workspaces/{id}").
func Get(path string) Builder {
	return Builder{Endpoint{method: http.MethodGet, path: path}}
}

// Post begins a POST endpoint for path.
func Post(path string) Builder {
	return Builder{Endpoint{method: http.MethodPost, path: path}}
}

// Put begins a PUT endpoint for path.
func Put(path string) Builder {
	return Builder{Endpoint{method: http.MethodPut, path: path}}
}

// Delete begins a DELETE endpoint for path.
func Delete(path string) Builder {
	return Builder{Endpoint{method: http.MethodDelete, path: path}}
}

// Summary sets the one-line summary.
func (b Builder) Summary(s string) Builder { b.e.summary = s; return b }

// Description sets an optional longer prose description.
func (b Builder) Description(s string) Builder { b.e.description = s; return b }

// Tags sets the grouping tags used on the docs page.
func (b Builder) Tags(t ...string) Builder { b.e.tags = t; return b }

// Accepts documents the request body with an example value, e.g.
// Accepts(proto.Workspace{}). The value's type is used to derive the
// JSON schema; the reference is compile-checked.
func (b Builder) Accepts(v any) Builder { b.e.request = v; return b }

// Responds documents the success response body with an example value.
func (b Builder) Responds(v any) Builder { b.e.response = v; return b }

// Status sets the success status code when it differs from 200, e.g.
// 202 for an accepted-but-async operation.
func (b Builder) Status(code int) Builder { b.e.status = code; return b }

// Fails documents status codes that return an error object.
func (b Builder) Fails(codes ...int) Builder { b.e.errors = codes; return b }

// SSE marks the endpoint as streaming text/event-stream.
func (b Builder) SSE() Builder { b.e.sse = true; return b }

// PathParam documents a path parameter. Path parameters are always
// required.
func (b Builder) PathParam(name, desc string) Builder {
	return b.param(Param{Name: name, In: "path", Type: "string", Description: desc, Required: true})
}

// QueryParam documents an optional query parameter.
func (b Builder) QueryParam(name, desc string) Builder {
	return b.param(Param{Name: name, In: "query", Type: "string", Description: desc})
}

// RequiredQueryParam documents a required query parameter.
func (b Builder) RequiredQueryParam(name, desc string) Builder {
	return b.param(Param{Name: name, In: "query", Type: "string", Description: desc, Required: true})
}

func (b Builder) param(p Param) Builder {
	b.e.params = append(b.e.params, p)
	return b
}

// Handle finalizes the endpoint with its HTTP handler.
func (b Builder) Handle(h http.HandlerFunc) Endpoint {
	b.e.handler = h
	return b.e
}

// Bind sets the endpoint's handler before its documentation, for
// helpers that build the handler from the same descriptor as the
// endpoint. Finish such a chain with [Builder.Build].
func (b Builder) Bind(h http.HandlerFunc) Builder {
	b.e.handler = h
	return b
}

// Build finalizes an endpoint whose handler was set with
// [Builder.Bind]. It panics when no handler was bound, which surfaces
// the mistake as soon as the route table is built.
func (b Builder) Build() Endpoint {
	if b.e.handler == nil {
		panic("apigen: Build called on " + b.e.method + " " + b.e.path + " without a bound handler")
	}
	return b.e
}

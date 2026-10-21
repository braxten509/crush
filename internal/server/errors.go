package server

import (
	"net/http"
	"slices"
)

// apiError maps the errors Match accepts to an HTTP status and a stable
// code for the error body (engine-design.md, section 0.1). Field, when
// set, fills the body's field from the error.
type apiError struct {
	Match  func(error) bool
	Status int
	Code   string
	Field  func(error) string
}

// pieceErrors lists the errors the build pieces add, each in its own
// endpoints_<piece>.go. errorInfo checks them before today's errors.
func pieceErrors() []apiError {
	return slices.Concat(lifecycleErrors(), liveErrors(), treeErrors(), shareErrors(), desktopErrors(), fallbackErrors())
}

// statusCode is the code of an error body written straight from a status
// (jsonError).
func statusCode(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "bad_request"
	case http.StatusNotFound:
		return "not_found"
	case http.StatusConflict:
		return "conflict"
	case http.StatusInternalServerError:
		return "internal"
	case http.StatusServiceUnavailable:
		return "shutting_down"
	default:
		return ""
	}
}

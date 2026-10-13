package server

import (
	"net/http"

	"github.com/charmbracelet/crush/internal/apigen"
)

// endpointHandler returns the handler registered for route, so tests
// exercise exactly what the mux serves. It panics for an unknown route.
func (c *controllerV1) endpointHandler(route apigen.Route) http.HandlerFunc {
	for _, e := range c.endpoints() {
		if e.Method() == route.RouteMethod() && e.Path() == route.RoutePath() {
			return e.Handler()
		}
	}
	panic("no endpoint for " + route.RouteMethod() + " " + route.RoutePath())
}

package server

import (
	"errors"
	"net/http"

	"github.com/charmbracelet/crush/internal/apigen"
	"github.com/charmbracelet/crush/internal/backend"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/charmbracelet/crush/internal/rpc"
)

// The desktop app's phase 5-6 engine gaps (desktop design 5-6, 9.1, G56)
// bind their routes, wrap their events and map their errors here.

// desktopEndpoints documents and routes the G56 endpoints.
func (c *controllerV1) desktopEndpoints() []apigen.Endpoint {
	return []apigen.Endpoint{
		route(c, rpc.PendingQuestion, c.pendingQuestion).
			Summary("Get the question a chat waits on").
			Description("Returns the pending question form of the chat or of one of its helpers, "+
				"so an app that missed the question_batch_request event can show it. 404 "+
				"no_pending_question when the chat waits on none.").
			Tags("questions").
			PathParam("id", "Workspace ID").
			RequiredQueryParam("session_id", "Session ID").
			Fails(400, 404).
			Build(),
	}
}

// wrapDesktopEvent wraps the G56 fan-in events for SSE. It reports false
// for an event that is not G56's.
func wrapDesktopEvent(any) (*pubsub.Payload, bool) { return nil, false }

// desktopErrors maps the G56 errors to their statuses and codes.
func desktopErrors() []apiError {
	is := func(target error) func(error) bool {
		return func(err error) bool { return errors.Is(err, target) }
	}
	return []apiError{
		{Match: is(backend.ErrNoPendingQuestion), Status: http.StatusNotFound, Code: "no_pending_question"},
	}
}

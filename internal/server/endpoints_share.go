package server

import (
	"errors"
	"net/http"

	"github.com/charmbracelet/crush/internal/apigen"
	"github.com/charmbracelet/crush/internal/backend"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/charmbracelet/crush/internal/rpc"
	"github.com/charmbracelet/crush/internal/secureentry"
)

// Piece D (phone sharing and secure entry, engine-design.md 5.7 and 5.8)
// binds its routes, wraps its events and maps its errors here.

// shareEndpoints documents and routes piece D's endpoints.
func (c *controllerV1) shareEndpoints() []apigen.Endpoint {
	return []apigen.Endpoint{
		route(c, rpc.RemoteStart, c.startRemote).
			Summary("Start Remote Control").
			Description("Starts the calling app's phone share, or returns its running share's "+
				"status: one share per app. A setup step answers state \"setup\" with the step; "+
				"any other failed start answers state \"off\" with error. launch_id is the "+
				"window's CRUSH_REMOTE_LAUNCH value, which the share reports to the launcher.").
			Tags("remote").
			PathParam("id", "Workspace ID").
			RequiredQueryParam("client_id", "Client ID (UUID)").
			Fails(400, 404, 409).
			Build(),

		route(c, rpc.RemoteStop, c.stopRemote).
			Summary("Stop Remote Control").
			Description("Stops the calling app's phone share. Only the app that started a share may stop it.").
			Tags("remote").
			PathParam("id", "Workspace ID").
			RequiredQueryParam("client_id", "Client ID (UUID)").
			Fails(400, 403, 404, 409).
			Build(),

		route(c, rpc.RemoteGet, c.getRemoteStatus).
			Summary("Get Remote Control status").
			Description("Describes the calling app's phone share; state \"off\" when it runs none.").
			Tags("remote").
			PathParam("id", "Workspace ID").
			RequiredQueryParam("client_id", "Client ID (UUID)").
			Fails(400, 404, 409).
			Build(),

		route(c, rpc.RemotePresence, c.setRemotePresence).
			Summary("Set Remote Control presence").
			Description("Tells the calling app's phone share what its window shows: the chat, "+
				"focus and plan mode. The share follows the chat of the app that started it.").
			Tags("remote").
			PathParam("id", "Workspace ID").
			RequiredQueryParam("client_id", "Client ID (UUID)").
			Fails(400, 403, 404, 409).
			Build(),

		route(c, rpc.RemoteActionDone, c.finishRemoteAction).
			Summary("Finish a phone action").
			Description("Reports the window's outcome of a remote_action event: an empty error "+
				"means done. For new_chat_send, session_id names the chat the window created and "+
				"shows; the engine then sends the phone's prompt there. Answer within 25 s.").
			Tags("remote").
			PathParam("id", "Workspace ID").
			PathParam("aid", "Phone action ID").
			RequiredQueryParam("client_id", "Client ID (UUID)").
			Fails(400, 403, 404, 409).
			Build(),

		route(c, rpc.ListSecureForms, c.listSecureForms).
			Summary("List secure entries").
			Description("Lists the open secure forms sent to the calling app, each field by its "+
				"label and status. Answers only on the engine's unix socket.").
			Tags("secure-entry").
			PathParam("id", "Workspace ID").
			RequiredQueryParam("client_id", "Client ID (UUID)").
			Fails(400, 403, 404, 409).
			Build(),

		apigen.For(rpc.SubmitSecureForm).
			Summary("Submit a secure entry").
			Description("Answers a secure form: the engine writes each value into its file. "+
				"Answers only on the engine's unix socket, and logs no part of the body. A field "+
				"that cannot be saved answers 422 secure_field_error with the field's question "+
				"ID; the form stays open.").
			Tags("secure-entry").
			PathParam("id", "Workspace ID").
			PathParam("fid", "Secure form ID").
			RequiredQueryParam("client_id", "Client ID (UUID)").
			Fails(400, 403, 404, 409, 422).
			Handle(c.handleSubmitSecureForm),

		route(c, rpc.CancelSecureForm, c.cancelSecureForm).
			Summary("Cancel a secure entry").
			Description("Closes a secure form without saving anything more. Answers only on the "+
				"engine's unix socket.").
			Tags("secure-entry").
			PathParam("id", "Workspace ID").
			PathParam("fid", "Secure form ID").
			RequiredQueryParam("client_id", "Client ID (UUID)").
			Fails(400, 403, 404, 409).
			Build(),
	}
}

// wrapShareEvent wraps piece D's fan-in events for SSE. It reports false
// for an event that is not D's. D has none: its events are engine-sent
// (backend.EngineEvent), which wrapEvent handles itself.
func wrapShareEvent(any) (*pubsub.Payload, bool) { return nil, false }

// shareErrors maps piece D's errors to their statuses and codes.
func shareErrors() []apiError {
	return []apiError{
		{Match: shareErrorIs(backend.ErrNotShareOwner), Status: http.StatusForbidden, Code: "not_owner"},
		{Match: shareErrorIs(backend.ErrShareOff), Status: http.StatusNotFound, Code: "share_off"},
		{Match: shareErrorIs(backend.ErrActionNotFound), Status: http.StatusNotFound, Code: "action_not_found"},
		{Match: shareErrorIs(backend.ErrSecureFormNotFound), Status: http.StatusNotFound, Code: "secure_form_not_found"},
		{Match: shareErrorIs(backend.ErrNotFormRecipient), Status: http.StatusForbidden, Code: "not_recipient"},
		{Match: shareErrorIs(backend.ErrLocalOnly), Status: http.StatusForbidden, Code: "local_only"},
		{
			Match:  func(err error) bool { return secureFieldError(err) != nil },
			Status: http.StatusUnprocessableEntity,
			Code:   "secure_field_error",
			Field:  func(err error) string { return secureFieldError(err).QuestionID },
		},
	}
}

// shareErrorIs matches the errors that are target.
func shareErrorIs(target error) func(error) bool {
	return func(err error) bool { return errors.Is(err, target) }
}

// secureFieldError returns the secure field error err holds, or nil.
func secureFieldError(err error) *secureentry.FieldError {
	var fieldErr *secureentry.FieldError
	if errors.As(err, &fieldErr) {
		return fieldErr
	}
	return nil
}

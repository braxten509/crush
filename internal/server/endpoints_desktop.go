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

		route(c, rpc.SetCPUAffinity, c.setCPUAffinity).
			Summary("Turn the agent CPUs on or off").
			Description("Saves options.agent_cpus_enabled and moves the engine and everything it "+
				"started onto options.agent_cpus, or back onto every CPU, as the terminal's toggle "+
				"does. cpus is the list applied, \"\" when off. A failed move answers 400 with the "+
				"terminal's text. Linux only.").
			Tags("system").
			Fails(400, 404, 500).
			Build(),

		route(c, rpc.ListThemes, c.listThemes).
			Summary("List themes").
			Description("The built-in and user themes both apps list, by name. source is "+
				"builtin or user; overridden marks a built-in a user file shadows.").
			Tags("themes").
			Build(),

		route(c, rpc.GetTheme, c.getTheme).
			Summary("Get a theme").
			Description("One theme, resolved: its 55 palette roles as the editor starts them, "+
				"the same roles as #rrggbb, and its code and Markdown colors. revision changes "+
				"whenever the theme's file does.").
			Tags("themes").
			PathParam("name", "Theme name").
			Fails(404, 422).
			Build(),

		route(c, rpc.ResolveTheme, c.resolveTheme).
			Summary("Preview unsaved theme colors").
			Description("Resolves a palette over a base without saving it. A bad color answers "+
				"422 with the terminal's text.").
			Tags("themes").
			Fails(400, 404, 422).
			Build(),

		route(c, rpc.SaveTheme, c.saveTheme).
			Summary("Save a theme's colors").
			Description("Writes the theme's file as the terminal's editor does, byte for byte, "+
				"and sends config_changed.").
			Tags("themes").
			PathParam("name", "Theme name").
			Fails(400, 404, 422, 500).
			Build(),

		route(c, rpc.CreateTheme, c.createTheme).
			Summary("Create a theme").
			Description("Saves a new theme with the resolved palette of base (the default theme "+
				"when base no longer resolves). No theme choice changes.").
			Tags("themes").
			Fails(400, 500).
			Build(),

		route(c, rpc.RenameTheme, c.renameTheme).
			Summary("Rename a theme").
			Description("Renames a user theme's file. When the terminal's options.tui.active_theme "+
				"names it, the setting follows; the file is moved back when that write fails.").
			Tags("themes").
			PathParam("name", "Theme name").
			Fails(400, 404, 500).
			Build(),

		route(c, rpc.DeleteTheme, c.deleteTheme).
			Summary("Delete a theme").
			Description("Deletes a user theme's file. No setting changes.").
			Tags("themes").
			PathParam("name", "Theme name").
			Fails(400, 404, 500).
			Build(),

		route(c, rpc.RevertTheme, c.revertTheme).
			Summary("Revert a built-in theme").
			Description("Removes the user file that shadows a built-in theme.").
			Tags("themes").
			PathParam("name", "Theme name").
			Fails(400, 500).
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
		{Match: is(backend.ErrCPUAffinity), Status: http.StatusBadRequest, Code: "bad_request"},
		{Match: is(backend.ErrThemeInvalid),Status: http.StatusBadRequest, Code: "bad_request"},
		{Match: is(backend.ErrThemeNotFound), Status: http.StatusNotFound, Code: "theme_not_found"},
		{Match: is(backend.ErrThemePalette), Status: http.StatusUnprocessableEntity, Code: "invalid_palette"},
	}
}

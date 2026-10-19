package server

import (
	"errors"
	"net/http"

	"github.com/charmbracelet/crush/internal/apigen"
	"github.com/charmbracelet/crush/internal/backend"
	"github.com/charmbracelet/crush/internal/proto"
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

		route(c, rpc.CustomCommands, c.customCommands).
			Summary("List custom commands").
			Description("The custom commands (Markdown files in the user's and the project's "+
				"commands folders) and the user-invocable skills, as the terminal's palette lists "+
				"them under User. skill is null for a command.").
			Tags("commands").
			PathParam("id", "Workspace ID").
			Fails(404, 500).
			Build(),

		route(c, rpc.ListServers, c.listServers).
			Summary("List the chats' server ports").
			Description("The TCP ports this computer's background work of every chat of the "+
				"project listens on, one row per port: the program holding it, the job's name "+
				"(or its program), its command and the chat. There is no event: apps poll.").
			Tags("jobs").
			PathParam("id", "Workspace ID").
			Fails(404, 500).
			Build(),

		route(c, rpc.SearchSkills, c.searchSkills).
			Summary("Search skills.sh").
			Description("Runs the skills.sh search of the terminal's skill browser and returns "+
				"its results as skills.sh sends them. A query under two characters answers 400; a "+
				"failed search answers 502 search_failed with the terminal's text.").
			Tags("skills").
			PathParam("id", "Workspace ID").
			RequiredQueryParam("q", "Search text").
			Fails(400, 404, 502).
			Build(),

		route(c, rpc.RefreshProviders, c.refreshProviders).
			Summary("Refresh the provider catalogs").
			Description("Runs what `crush update-providers` runs for the catwalk and hyper sources, " +
				"then reloads every workspace's config and sends config_changed. Each source's " +
				"outcome is in results, with the error text when it failed.").
			Tags("config").
			Build(),

		route(c, rpc.HyperCredits, c.hyperCredits).
			Summary("Get the Hyper credits").
			Description("Fetches the Hyper credits balance as the terminal does: an expired OAuth " +
				"token is refreshed first, then hyper.FetchCredits runs. credits is null when Hyper " +
				"is not configured or the account shows no balance; a failed fetch answers 502 " +
				"credits_failed.").
			Tags("config").
			Fails(502).
			Build(),

		apigen.For(rpc.SetCPUAffinity).
			Summary("Turn the agent CPUs on or off").
			Description("Saves options.agent_cpus_enabled and moves the engine and everything it "+
				"started onto options.agent_cpus, or back onto every CPU, as the terminal's toggle "+
				"does. cpus is the list applied, \"\" when off. A failed move answers 400 with the "+
				"terminal's text. Linux only. Answers only on the engine's unix socket.").
			Tags("system").
			Fails(400, 403, 404, 500).
			Handle(serve(c, rpc.SetCPUAffinity, c.setCPUAffinity)),

		apigen.For(rpc.StartAuthFlow).
			Summary("Start a sign-in").
			Description("Starts `crush login`'s flow for platform (hyper, copilot, openai or grok) and "+
				"returns the URL to open and the code to show; code_entry is set when a code pasted "+
				"from the provider's page can finish it. The flow then reports through auth_flow "+
				"events (waiting, saving, done, failed); the engine saves the token as `crush login` "+
				"does and fetches the account's models before done. Answers only on the engine's "+
				"unix socket; nothing of it is logged.").
			Tags("auth").
			PathParam("id", "Workspace ID").
			Fails(400, 403, 404, 500, 502).
			Handle(c.localOnly(serve(c, rpc.StartAuthFlow, c.startAuthFlow))),

		apigen.For(rpc.SendAuthCode).
			Summary("Finish a sign-in with a code").
			Description("Sends the code pasted from the provider's page (Grok's browser flow). The "+
				"outcome arrives as auth_flow; a wrong code fails the flow. Answers only on the "+
				"engine's unix socket; nothing of the body is logged.").
			Tags("auth").
			PathParam("id", "Workspace ID").
			PathParam("flow_id", "Sign-in flow ID").
			Fails(400, 403, 404).
			Handle(c.localOnly(c.handleSendAuthCode)),

		apigen.For(rpc.CancelAuthFlow).
			Summary("Cancel a sign-in").
			Description("Stops a running sign-in; no event follows and nothing is saved. A flow "+
				"already saving its token answers 409 auth_flow_saving. Answers only on the "+
				"engine's unix socket.").
			Tags("auth").
			PathParam("id", "Workspace ID").
			PathParam("flow_id", "Sign-in flow ID").
			Fails(403, 404, 409).
			Handle(c.localOnly(serve(c, rpc.CancelAuthFlow, c.cancelAuthFlow))),

		apigen.For(rpc.VerifyKey).
			Summary("Check an API key").
			Description("Tests an API key against the provider as the terminal's API key sheet "+
				"does, without saving it: ok, or the reason in error. Answers only on the engine's "+
				"unix socket; the key is never logged.").
			Tags("auth").
			PathParam("id", "Workspace ID").
			Fails(400, 403, 404).
			Handle(c.localOnly(c.handleVerifyKey)),

		route(c, rpc.ListThemes, c.listThemes).
			Summary("List themes").
			Description("The built-in and user themes both apps list, by name. source is " +
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
func wrapDesktopEvent(ev any) (*pubsub.Payload, bool) {
	switch e := ev.(type) {
	case pubsub.Event[proto.AuthFlowEvent]:
		return envelope(pubsub.PayloadTypeAuthFlow, e), true
	default:
		return nil, false
	}
}

// desktopErrors maps the G56 errors to their statuses and codes.
func desktopErrors() []apiError {
	is := func(target error) func(error) bool {
		return func(err error) bool { return errors.Is(err, target) }
	}
	return []apiError{
		{Match: is(backend.ErrNoPendingQuestion), Status: http.StatusNotFound, Code: "no_pending_question"},
		{Match: is(backend.ErrCPUAffinity), Status: http.StatusBadRequest, Code: "bad_request"},
		{Match: is(backend.ErrThemeInvalid), Status: http.StatusBadRequest, Code: "bad_request"},
		{Match: is(backend.ErrThemeNotFound), Status: http.StatusNotFound, Code: "theme_not_found"},
		{Match: is(backend.ErrThemePalette), Status: http.StatusUnprocessableEntity, Code: "invalid_palette"},
		{Match: is(backend.ErrSkillsQuery), Status: http.StatusBadRequest, Code: "bad_request"},
		{Match: is(backend.ErrSkillsSearch), Status: http.StatusBadGateway, Code: "search_failed"},
		{Match: is(backend.ErrHyperCredits), Status: http.StatusBadGateway, Code: "credits_failed"},
		{Match: is(backend.ErrAuthFlowNotFound), Status: http.StatusNotFound, Code: "auth_flow_not_found"},
		{Match: is(backend.ErrAuthFlowSaving), Status: http.StatusConflict, Code: "auth_flow_saving"},
		{Match: is(backend.ErrAuthStart), Status: http.StatusBadGateway, Code: "auth_failed"},
		{Match: is(backend.ErrAuthPlatform), Status: http.StatusBadRequest, Code: "bad_request"},
		{Match: is(backend.ErrAuthNoCode), Status: http.StatusBadRequest, Code: "bad_request"},
		{Match: is(backend.ErrAuthProvider), Status: http.StatusNotFound, Code: "provider_not_found"},
	}
}

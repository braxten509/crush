package server

import (
	"errors"
	"net/http"

	"github.com/charmbracelet/crush/internal/agent"
	"github.com/charmbracelet/crush/internal/apigen"
	"github.com/charmbracelet/crush/internal/backend"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/charmbracelet/crush/internal/rpc"
)

// Piece B (live work, ownership and the smaller gaps, engine-design.md 5.3,
// 5.4, 5.5 and 5.9) binds its routes, wraps its events and maps its errors
// here.

// liveEndpoints documents and routes piece B's endpoints.
func (c *controllerV1) liveEndpoints() []apigen.Endpoint {
	return []apigen.Endpoint{
		route(c, rpc.ListHelpers, c.listHelpers).
			Summary("List a chat's helpers").
			Description("Lists the chat's helpers (sub-agents), running or ended. A helper's own "+
				"helpers are listed under its child chat. The helper event can be dropped, so "+
				"apps list again on reconnect, when a chat opens, and every second while a "+
				"helper of the shown chat runs.").
			Tags("helpers").
			PathParam("id", "Workspace ID").
			PathParam("sid", "Session ID").
			Fails(404, 500).
			Build(),

		route(c, rpc.StopHelper, c.stopHelper).
			Summary("Stop a chat's helper").
			Description("Stops the helper the way the user's stop does: its parent chat is told. "+
				"A helper of another chat answers 404 helper_not_found and keeps running; an "+
				"ended helper answers 200 and is left as it is.").
			Tags("helpers").
			PathParam("id", "Workspace ID").
			PathParam("sid", "Session ID").
			PathParam("hid", "Helper (task) ID").
			QueryParam("client_id", "Client ID (UUID) of the calling app").
			Fails(400, 404, 409, 500).
			Build(),

		route(c, rpc.ListJobs, c.listJobs).
			Summary("List a chat's background jobs").
			Description("Lists the commands running in the background for the chat. There is no "+
				"job event: apps poll once a second for the chat on screen.").
			Tags("jobs").
			PathParam("id", "Workspace ID").
			PathParam("sid", "Session ID").
			Fails(404, 500).
			Build(),

		route(c, rpc.KillJob, c.killJob).
			Summary("Kill a chat's background job").
			Description("Exactly one of job_id and pid is set. A job that is not in the chat's "+
				"list answers 404 job_not_found and keeps running.").
			Tags("jobs").
			PathParam("id", "Workspace ID").
			PathParam("sid", "Session ID").
			QueryParam("client_id", "Client ID (UUID) of the calling app").
			Fails(400, 404, 409, 500).
			Build(),

		route(c, rpc.MoveToBackground, c.moveToBackground).
			Summary("Move the chat's running command to the background").
			Description("Ctrl+B. moved is false, not an error, when nothing could move.").
			Tags("jobs").
			PathParam("id", "Workspace ID").
			PathParam("sid", "Session ID").
			QueryParam("client_id", "Client ID (UUID) of the calling app").
			Fails(400, 404, 409, 500).
			Build(),

		route(c, rpc.AgentCapabilities, c.agentCapabilities).
			Summary("Get what the current coder model can do").
			Description("can_background tells whether Ctrl+B can move the model's running "+
				"commands to the background, as the terminal's hint does.").
			Tags("agent").
			PathParam("id", "Workspace ID").
			Fails(404, 500).
			Build(),

		route(c, rpc.EnvIsSet, c.envIsSet).
			Summary("Check which environment variables are set").
			Description("Tells, per bare name, whether the variable has a non-empty value in the "+
				"environment the engine's config resolver reads. Values are never returned "+
				"and nothing is expanded or run. At most 64 names, each matching "+
				"^[A-Za-z_][A-Za-z0-9_]{0,127}$.").
			Tags("workspaces").
			PathParam("id", "Workspace ID").
			Fails(400, 404, 500).
			Build(),

		route(c, rpc.MemoryStatus, c.memoryStatus).
			Summary("Get memory use and the memory box's limit").
			Tags("memory").
			Fails(500).
			Build(),

		route(c, rpc.ClaimMemoryNotice, c.claimMemoryNotice).
			Summary("Claim the memory pop-up").
			Description("claimed is true for one caller per 10 minutes, across every Crush " +
				"window, terminal or app.").
			Tags("memory").
			Fails(500).
			Build(),

		route(c, rpc.SetMemoryLimit, c.setMemoryLimit).
			Summary("Set the memory box's limit").
			Description("Parses, saves and applies the limit as the terminal's memory-limit dialog "+
				"does. An empty value is no limit. A refused value answers 400 with the dialog's "+
				"text; notice is the dialog's outcome text.").
			Tags("memory").
			Fails(400, 500).
			Build(),

		route(c, rpc.CheckCLIUpdates, c.checkCLIUpdates).
			Summary("Look for AI tool updates").
			Description("An automatic check (manual=false) runs at most hourly across every Crush "+
				"and leaves out declined versions; skipped is true when it is not due.").
			Tags("updates").
			QueryParam("manual", "true for a check the user asked for").
			Fails(400, 500).
			Build(),

		route(c, rpc.DeclineCLIUpdates, c.declineCLIUpdates).
			Summary("Skip AI tool updates").
			Description("Skips the versions the last check offered for the given binaries until "+
				"newer ones come out. Binaries that check did not offer are ignored.").
			Tags("updates").
			Fails(400, 500).
			Build(),

		route(c, rpc.InstallCLIUpdates, c.installCLIUpdates).
			Summary("Install AI tool updates").
			Description("Starts the updates for the given binaries, all at once, from the last "+
				"check (checking again when it is older than 10 minutes). The calling app gets "+
				"the results as a cli_updates_installed event.").
			Tags("updates").
			RequiredQueryParam("client_id", "Client ID (UUID) of the app that gets the results").
			Fails(400, 409, 500).
			Build(),

		route(c, rpc.CheckCrushUpdate, c.checkCrushUpdate).
			Summary("Look for a Crush update").
			Description("Looks for a stable upstream release newer than this fork build. Merging, "+
				"building and installing it stay in `crush self-update merge|build|install`.").
			Tags("updates").
			QueryParam("manual", "true for a check the user asked for").
			Fails(400, 500).
			Build(),

		route(c, rpc.DeclineCrushUpdate, c.declineCrushUpdate).
			Summary("Skip a Crush release").
			Tags("updates").
			Fails(400, 500).
			Build(),

		route(c, rpc.Limits, c.limits).
			Summary("Get the AI tools' usage limits").
			Description("state is checking until the first reading, ready once one arrived (with " +
				"read_at), and error while there is none and the last refresh failed. Changes " +
				"arrive as limits events.").
			Tags("limits").
			Fails(500).
			Build(),

		route(c, rpc.RefreshLimits, c.refreshLimits).
			Summary("Refresh an AI tool's usage limits").
			Description("Starts the refresh and answers at once; the result arrives as a limits "+
				"event.").
			Tags("limits").
			PathParam("kind", "The AI tool's catwalk type").
			Fails(400, 500).
			Build(),
	}
}

// wrapLiveEvent wraps piece B's fan-in events for SSE. It reports false for
// an event that is not B's.
func wrapLiveEvent(ev any) (*pubsub.Payload, bool) {
	switch e := ev.(type) {
	case pubsub.Event[agent.Task]:
		return envelope(pubsub.PayloadTypeHelper, pubsub.Event[proto.Helper]{
			Type:    e.Type,
			Payload: proto.HelperFromTask(e.Payload),
		}), true
	default:
		return nil, false
	}
}

// liveErrors maps piece B's errors to their statuses and codes.
func liveErrors() []apiError {
	is := func(target error) func(error) bool {
		return func(err error) bool { return errors.Is(err, target) }
	}
	return []apiError{
		{Match: is(backend.ErrHelperNotFound), Status: http.StatusNotFound, Code: "helper_not_found"},
		{Match: is(backend.ErrJobNotFound), Status: http.StatusNotFound, Code: "job_not_found"},
		{Match: is(backend.ErrBadEnvName), Status: http.StatusBadRequest, Code: "bad_request"},
		{Match: is(backend.ErrMemoryLimitInvalid), Status: http.StatusBadRequest, Code: "bad_request"},
	}
}

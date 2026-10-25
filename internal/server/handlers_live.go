package server

import (
	"net/http"

	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/rpc"
)

// Handlers of piece B's routes (engine-design.md 5.3, 5.4 and 5.9).

// maxRequestIDLen is the longest request_id a shell command may carry.
const maxRequestIDLen = 64

// liveClientID reads an optional client_id: "" when absent, 400 when it is
// not a UUID, and 409 client_not_attached when that app is not registered
// on the workspace (section 0.1).
func (c *controllerV1) liveClientID(r *http.Request) (string, error) {
	clientID, err := c.optionalClientID(r)
	if err != nil || clientID == "" {
		return clientID, err
	}
	return clientID, c.backend.CheckClientRegistered(r.PathValue("id"), clientID)
}

// manualQuery reads the manual query parameter: true, false or absent.
func manualQuery(r *http.Request) (bool, error) {
	switch r.URL.Query().Get("manual") {
	case "", "false":
		return false, nil
	case "true":
		return true, nil
	default:
		return false, badRequest("manual must be true or false")
	}
}

// listHelpers lists the chat's helpers.
func (c *controllerV1) listHelpers(r *http.Request, _ rpc.Empty) ([]proto.Helper, error) {
	return c.backend.ListHelpers(r.PathValue("id"), r.PathValue("sid"))
}

// stopHelper stops one of the chat's helpers.
func (c *controllerV1) stopHelper(r *http.Request, _ rpc.Empty) (rpc.Empty, error) {
	if _, err := c.liveClientID(r); err != nil {
		return rpc.Empty{}, err
	}
	return rpc.Empty{}, c.backend.StopHelper(r.PathValue("id"), r.PathValue("sid"), r.PathValue("hid"))
}

// listJobs lists the chat's background jobs.
func (c *controllerV1) listJobs(r *http.Request, _ rpc.Empty) ([]proto.Job, error) {
	return c.backend.ListJobs(r.PathValue("id"), r.PathValue("sid"))
}

// killJob ends one of the chat's background jobs.
func (c *controllerV1) killJob(r *http.Request, ref proto.JobRef) (rpc.Empty, error) {
	if _, err := c.liveClientID(r); err != nil {
		return rpc.Empty{}, err
	}
	if (ref.JobID == "") == (ref.PID == 0) || ref.PID < 0 {
		return rpc.Empty{}, badRequest("set exactly one of job_id and pid")
	}
	return rpc.Empty{}, c.backend.KillJob(r.PathValue("id"), r.PathValue("sid"), ref)
}

// moveToBackground moves the chat's running command to the background.
func (c *controllerV1) moveToBackground(r *http.Request, _ rpc.Empty) (proto.BackgroundResult, error) {
	if _, err := c.liveClientID(r); err != nil {
		return proto.BackgroundResult{}, err
	}
	moved, err := c.backend.MoveToBackground(r.PathValue("id"), r.PathValue("sid"))
	return proto.BackgroundResult{Moved: moved}, err
}

// agentCapabilities tells what the ?session= chat's own model can do.
func (c *controllerV1) agentCapabilities(r *http.Request, _ rpc.Empty) (proto.AgentCapabilities, error) {
	return c.backend.AgentCapabilities(r.PathValue("id"), r.URL.Query().Get("session"))
}

// envIsSet tells which environment variables are set, never their values.
func (c *controllerV1) envIsSet(r *http.Request, req proto.EnvCheck) (proto.EnvCheckResult, error) {
	set, err := c.backend.EnvIsSet(r.PathValue("id"), req.Names)
	return proto.EnvCheckResult{Set: set}, err
}

// memoryStatus reports memory use and the memory box's limit.
func (c *controllerV1) memoryStatus(*http.Request, rpc.Empty) (proto.MemoryStatus, error) {
	return c.backend.MemoryStatus(), nil
}

// claimMemoryNotice claims the memory pop-up for the caller.
func (c *controllerV1) claimMemoryNotice(*http.Request, rpc.Empty) (proto.ClaimResult, error) {
	return c.backend.ClaimMemoryNotice(), nil
}

// setMemoryLimit sets the memory box's limit.
func (c *controllerV1) setMemoryLimit(_ *http.Request, req proto.MemoryLimitRequest) (proto.MemoryLimitResult, error) {
	return c.backend.SetMemoryLimit(req.Value)
}

// checkCLIUpdates looks for AI tool updates.
func (c *controllerV1) checkCLIUpdates(r *http.Request, _ rpc.Empty) (proto.CLIUpdateCheck, error) {
	manual, err := manualQuery(r)
	if err != nil {
		return proto.CLIUpdateCheck{}, err
	}
	return c.backend.CheckCLIUpdates(r.Context(), manual), nil
}

// declineCLIUpdates skips the offered AI tool updates.
func (c *controllerV1) declineCLIUpdates(_ *http.Request, req proto.CLIUpdateBins) (rpc.Empty, error) {
	c.backend.DeclineCLIUpdates(req.Bins)
	return rpc.Empty{}, nil
}

// installCLIUpdates starts AI tool updates; the results go to the caller.
func (c *controllerV1) installCLIUpdates(r *http.Request, req proto.CLIUpdateBins) (rpc.Empty, error) {
	clientID, err := c.clientID(r)
	if err != nil {
		return rpc.Empty{}, err
	}
	return rpc.Empty{}, c.backend.InstallCLIUpdates(clientID, req.Bins)
}

// checkCrushUpdate looks for a Crush update.
func (c *controllerV1) checkCrushUpdate(r *http.Request, _ rpc.Empty) (proto.CrushUpdateCheck, error) {
	if _, err := manualQuery(r); err != nil {
		return proto.CrushUpdateCheck{}, err
	}
	return c.backend.CheckCrushUpdate(r.Context()), nil
}

// declineCrushUpdate skips a Crush release.
func (c *controllerV1) declineCrushUpdate(_ *http.Request, req proto.CrushTag) (rpc.Empty, error) {
	if req.Tag == "" {
		return rpc.Empty{}, badRequest("tag is required")
	}
	c.backend.DeclineCrushUpdate(req.Tag)
	return rpc.Empty{}, nil
}

// limits reports every AI tool's usage limits.
func (c *controllerV1) limits(*http.Request, rpc.Empty) (proto.LimitsSnapshot, error) {
	return c.backend.Limits(), nil
}

// refreshLimits starts a refresh of one AI tool's usage limits.
func (c *controllerV1) refreshLimits(r *http.Request, _ rpc.Empty) (rpc.Empty, error) {
	if !c.backend.RefreshLimits(r.PathValue("kind")) {
		return rpc.Empty{}, badRequest("This CLI does not report usage limits")
	}
	return rpc.Empty{}, nil
}

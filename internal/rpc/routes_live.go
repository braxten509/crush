package rpc

import (
	"net/http"

	"github.com/charmbracelet/crush/internal/proto"
)

// Helper endpoints (engine-design.md 5.3).
var (
	ListHelpers = Op[Empty, []proto.Helper]{Method: get, Path: "/v1/workspaces/{id}/sessions/{sid}/helpers"}
	StopHelper  = Op[Empty, Empty]{Method: post, Path: "/v1/workspaces/{id}/sessions/{sid}/helpers/{hid}/stop"}
)

// Background job endpoints (engine-design.md 5.4).
var (
	ListJobs          = Op[Empty, []proto.Job]{Method: get, Path: "/v1/workspaces/{id}/sessions/{sid}/jobs"}
	KillJob           = Op[proto.JobRef, Empty]{Method: post, Path: "/v1/workspaces/{id}/sessions/{sid}/jobs/kill"}
	MoveToBackground  = Op[Empty, proto.BackgroundResult]{Method: post, Path: "/v1/workspaces/{id}/sessions/{sid}/background"}
	AgentCapabilities = Op[Empty, proto.AgentCapabilities]{Method: get, Path: "/v1/workspaces/{id}/agent/capabilities"}
)

// Smaller gaps (engine-design.md 5.9).
var (
	EnvIsSet = Op[proto.EnvCheck, proto.EnvCheckResult]{Method: post, Path: "/v1/workspaces/{id}/env/is-set"}

	MemoryStatus      = Op[Empty, proto.MemoryStatus]{Method: get, Path: "/v1/memory"}
	ClaimMemoryNotice = Op[Empty, proto.ClaimResult]{Method: post, Path: "/v1/memory/claim-notice"}
	SetMemoryLimit    = Op[proto.MemoryLimitRequest, proto.MemoryLimitResult]{Method: put, Path: "/v1/memory/limit"}

	CheckCLIUpdates    = Op[Empty, proto.CLIUpdateCheck]{Method: get, Path: "/v1/updates/clis"}
	DeclineCLIUpdates  = Op[proto.CLIUpdateBins, Empty]{Method: post, Path: "/v1/updates/clis/decline"}
	InstallCLIUpdates  = Op[proto.CLIUpdateBins, Empty]{Method: post, Path: "/v1/updates/clis/install", Status: http.StatusAccepted}
	CheckCrushUpdate   = Op[Empty, proto.CrushUpdateCheck]{Method: get, Path: "/v1/updates/crush"}
	DeclineCrushUpdate = Op[proto.CrushTag, Empty]{Method: post, Path: "/v1/updates/crush/decline"}

	Limits        = Op[Empty, proto.LimitsSnapshot]{Method: get, Path: "/v1/limits"}
	RefreshLimits = Op[Empty, Empty]{Method: post, Path: "/v1/limits/{kind}/refresh", Status: http.StatusAccepted}
)

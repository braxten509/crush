package rpc

import "github.com/charmbracelet/crush/internal/proto"

// Phone sharing endpoints (engine-design.md 5.7), one share per chat. Each
// takes the calling app's client_id.
var (
	RemoteStart      = Op[proto.RemoteStart, proto.RemoteStatus]{Method: post, Path: "/v1/workspaces/{id}/remote/start"}
	RemoteStop       = Op[proto.RemoteStop, Empty]{Method: post, Path: "/v1/workspaces/{id}/remote/stop"}
	RemoteGet        = Op[Empty, []proto.RemoteStatus]{Method: get, Path: "/v1/workspaces/{id}/remote"}
	RemotePresence   = Op[proto.RemotePresence, Empty]{Method: post, Path: "/v1/workspaces/{id}/remote/presence"}
	RemoteActionDone = Op[proto.RemoteActionDone, Empty]{Method: post, Path: "/v1/workspaces/{id}/remote/actions/{aid}/done"}
)

// Secure entry endpoints (engine-design.md 5.8). Each takes the calling
// app's client_id, and each answers only on the engine's unix socket.
var (
	ListSecureForms  = Op[Empty, []proto.SecureForm]{Method: get, Path: "/v1/workspaces/{id}/secure-forms"}
	SubmitSecureForm = Op[proto.SecureSubmit, Empty]{Method: post, Path: "/v1/workspaces/{id}/secure-forms/{fid}/submit"}
	CancelSecureForm = Op[Empty, Empty]{Method: post, Path: "/v1/workspaces/{id}/secure-forms/{fid}/cancel"}
)

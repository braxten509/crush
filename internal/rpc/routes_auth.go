package rpc

import "github.com/charmbracelet/crush/internal/proto"

// Sign-in routes for the desktop app (desktop design 5-6, 2.4; G-5). They
// answer on the engine's unix socket only.
var (
	// StartAuthFlow starts a platform's sign-in.
	StartAuthFlow = Op[proto.AuthFlowRequest, proto.AuthFlow]{Method: post, Path: "/v1/workspaces/{id}/auth/flows"}

	// SendAuthCode finishes a sign-in with a code pasted from the
	// provider's page.
	SendAuthCode = Op[proto.AuthCodeRequest, Empty]{Method: post, Path: "/v1/workspaces/{id}/auth/flows/{flow_id}/code"}

	// CancelAuthFlow stops a sign-in.
	CancelAuthFlow = Op[Empty, Empty]{Method: post, Path: "/v1/workspaces/{id}/auth/flows/{flow_id}/cancel"}

	// VerifyKey tests an API key against a provider without saving it.
	VerifyKey = Op[proto.VerifyKeyRequest, proto.VerifyKey]{Method: post, Path: "/v1/workspaces/{id}/auth/verify-key"}

	// GetAuthState says which agent CLIs hold their own login here.
	GetAuthState = Op[Empty, proto.AuthStateResponse]{Method: get, Path: "/v1/workspaces/{id}/auth/state"}
)

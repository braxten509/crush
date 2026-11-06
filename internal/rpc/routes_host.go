package rpc

import "github.com/charmbracelet/crush/internal/proto"

// Host application endpoints (upstream v0.100.0): an application that runs
// Crush in one of its panes (an editor, for example) sets a theme for every
// client and tells the agent what it has in view.
var (
	GetHostTheme   = Op[Empty, proto.HostThemeState]{Method: get, Path: "/v1/workspaces/{id}/theme"}
	SetHostTheme   = Op[proto.HostThemeRequest, proto.HostThemeState]{Method: post, Path: "/v1/workspaces/{id}/theme"}
	GetHostContext = Op[Empty, proto.HostContext]{Method: get, Path: "/v1/workspaces/{id}/host-context"}
	SetHostContext = Op[proto.HostContext, Empty]{Method: post, Path: "/v1/workspaces/{id}/host-context"}
	// AddSessionNote keeps text for a session's next prompt; it starts no
	// run and is not a chat message.
	AddSessionNote = Op[proto.SessionNote, Empty]{Method: post, Path: "/v1/workspaces/{id}/agent/sessions/{sid}/notes"}
)

// Permission mode and credential prompt endpoints (upstream v0.100.0).
// SetPermissionMode is the three-level successor of SetPermissionsSkip,
// which stays for apps that only know the on/off form.
var (
	SetPermissionMode = Op[proto.PermissionSetModeRequest, Empty]{Method: post, Path: "/v1/workspaces/{id}/permissions/mode"}
	AnswerPinentry    = Op[proto.PinentryAnswer, proto.PinentryResponse]{Method: post, Path: "/v1/workspaces/{id}/pinentry/answer"}
	CancelPinentry    = Op[proto.PinentryCancel, proto.PinentryResponse]{Method: post, Path: "/v1/workspaces/{id}/pinentry/cancel"}
	AnswerSSH         = Op[proto.SSHAnswer, proto.SSHResponse]{Method: post, Path: "/v1/workspaces/{id}/ssh/answer"}
	CancelSSH         = Op[proto.SSHCancel, proto.SSHResponse]{Method: post, Path: "/v1/workspaces/{id}/ssh/cancel"}
)

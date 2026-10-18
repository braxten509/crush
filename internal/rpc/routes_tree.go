package rpc

import "github.com/charmbracelet/crush/internal/proto"

// Tree endpoints (engine-design.md 5.6).
var (
	GetTree          = Op[Empty, proto.Tree]{Method: get, Path: "/v1/workspaces/{id}/sessions/{sid}/tree"}
	GetTreeMessage   = Op[Empty, proto.Message]{Method: get, Path: "/v1/workspaces/{id}/sessions/{sid}/tree/messages/{mid}"}
	LabelTree        = Op[proto.TreeLabelRequest, proto.TreeResult]{Method: post, Path: "/v1/workspaces/{id}/sessions/{sid}/tree/label"}
	SwitchTree       = Op[proto.TreeMoveRequest, proto.TreeResult]{Method: post, Path: "/v1/workspaces/{id}/sessions/{sid}/tree/switch"}
	NavigateTree     = Op[proto.TreeMoveRequest, proto.TreeResult]{Method: post, Path: "/v1/workspaces/{id}/sessions/{sid}/tree/navigate"}
	CopyTree         = Op[proto.TreeCopyRequest, proto.TreeCopyResult]{Method: post, Path: "/v1/workspaces/{id}/sessions/{sid}/tree/copy"}
	PreviewFiles     = Op[proto.FilePreviewRequest, proto.FilePlan]{Method: post, Path: "/v1/workspaces/{id}/sessions/{sid}/files/preview"}
	PreviewUndoFiles = Op[Empty, proto.FilePlan]{Method: post, Path: "/v1/workspaces/{id}/sessions/{sid}/files/undo-preview"}
	UndoFiles        = Op[proto.FileUndoRequest, proto.FileRestoreResult]{Method: post, Path: "/v1/workspaces/{id}/sessions/{sid}/files/undo"}
)

// Branch endpoints (engine-design.md 5.11).
var (
	CreateBranch  = Op[proto.BranchRequest, proto.Branch]{Method: post, Path: "/v1/workspaces/{id}/sessions/{sid}/branches"}
	ListChatLinks = Op[Empty, []proto.ChatLink]{Method: get, Path: "/v1/workspaces/{id}/chat-links"}
)

package server

import (
	"net/http"

	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/rpc"
)

// treeCaller checks the optional client_id of a tree or branch route: a
// malformed one is a 400, and one whose app is not registered on the
// workspace a 409 (engine-design.md 0.1).
func (c *controllerV1) treeCaller(r *http.Request) error {
	clientID, err := c.optionalClientID(r)
	if err != nil {
		return err
	}
	return c.backend.TreeClient(r.PathValue("id"), clientID)
}

// getTree returns a chat's tree.
func (c *controllerV1) getTree(r *http.Request, _ rpc.Empty) (proto.Tree, error) {
	return c.backend.Tree(r.Context(), r.PathValue("id"), r.PathValue("sid"))
}

// getTreeMessage returns one whole message of a chat's tree.
func (c *controllerV1) getTreeMessage(r *http.Request, _ rpc.Empty) (proto.Message, error) {
	return c.backend.TreeMessage(r.Context(), r.PathValue("id"), r.PathValue("sid"), r.PathValue("mid"))
}

// labelTree labels a message of a chat's tree.
func (c *controllerV1) labelTree(r *http.Request, req proto.TreeLabelRequest) (proto.TreeResult, error) {
	if err := c.treeCaller(r); err != nil {
		return proto.TreeResult{}, err
	}
	return c.backend.LabelTree(r.Context(), r.PathValue("id"), r.PathValue("sid"), req)
}

// switchTree moves a chat to another branch.
func (c *controllerV1) switchTree(r *http.Request, req proto.TreeMoveRequest) (proto.TreeResult, error) {
	if err := c.treeCaller(r); err != nil {
		return proto.TreeResult{}, err
	}
	return c.backend.SwitchTree(r.Context(), r.PathValue("id"), r.PathValue("sid"), req)
}

// navigateTree moves a chat to another branch, optionally with a summary.
func (c *controllerV1) navigateTree(r *http.Request, req proto.TreeMoveRequest) (proto.TreeResult, error) {
	if err := c.treeCaller(r); err != nil {
		return proto.TreeResult{}, err
	}
	return c.backend.NavigateTree(r.Context(), r.PathValue("id"), r.PathValue("sid"), req)
}

// copyTree forks or clones a chat.
func (c *controllerV1) copyTree(r *http.Request, req proto.TreeCopyRequest) (proto.TreeCopyResult, error) {
	if err := c.treeCaller(r); err != nil {
		return proto.TreeCopyResult{}, err
	}
	return c.backend.CopyTree(r.Context(), r.PathValue("id"), r.PathValue("sid"), req)
}

// previewFiles plans the file restore of a move.
func (c *controllerV1) previewFiles(r *http.Request, req proto.FilePreviewRequest) (proto.FilePlan, error) {
	if err := c.treeCaller(r); err != nil {
		return proto.FilePlan{}, err
	}
	return c.backend.PreviewFiles(r.Context(), r.PathValue("id"), r.PathValue("sid"), req)
}

// previewUndoFiles plans undoing the chat's last file restore.
func (c *controllerV1) previewUndoFiles(r *http.Request, _ rpc.Empty) (proto.FilePlan, error) {
	if err := c.treeCaller(r); err != nil {
		return proto.FilePlan{}, err
	}
	return c.backend.PreviewUndoFiles(r.Context(), r.PathValue("id"), r.PathValue("sid"))
}

// undoFiles applies a kept file plan.
func (c *controllerV1) undoFiles(r *http.Request, req proto.FileUndoRequest) (proto.FileRestoreResult, error) {
	if err := c.treeCaller(r); err != nil {
		return proto.FileRestoreResult{}, err
	}
	return c.backend.UndoFiles(r.Context(), r.PathValue("id"), r.PathValue("sid"), req.PlanID)
}

// createBranch copies a chat through a highlighted message into a branch.
func (c *controllerV1) createBranch(r *http.Request, req proto.BranchRequest) (proto.Branch, error) {
	if err := c.treeCaller(r); err != nil {
		return proto.Branch{}, err
	}
	return c.backend.CreateBranch(r.Context(), r.PathValue("id"), r.PathValue("sid"), req)
}

// listChatLinks lists the links between the workspace's chats.
func (c *controllerV1) listChatLinks(r *http.Request, _ rpc.Empty) ([]proto.ChatLink, error) {
	return c.backend.ChatLinks(r.Context(), r.PathValue("id"))
}

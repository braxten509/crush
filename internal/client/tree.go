package client

import (
	"context"
	"net/url"

	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/rpc"
)

// Tree actions and branch copy (engine-design.md 5.6 and 5.11). Calls that
// change something send this client's ID.

func (c *Client) callerQuery() url.Values {
	return url.Values{"client_id": []string{c.clientID}}
}

// Tree reads a chat's tree and its revision.
func (c *Client) Tree(ctx context.Context, id, sessionID string) (proto.Tree, error) {
	return newCall(c, rpc.GetTree, "get session tree", "session tree").at(id, sessionID).get(ctx)
}

// TreeMessage reads one whole message of a chat's tree.
func (c *Client) TreeMessage(ctx context.Context, id, sessionID, messageID string) (proto.Message, error) {
	return newCall(c, rpc.GetTreeMessage, "get tree message", "tree message").at(id, sessionID, messageID).get(ctx)
}

// LabelTree labels a message of a chat's tree.
func (c *Client) LabelTree(ctx context.Context, id, sessionID string, req proto.TreeLabelRequest) (proto.TreeResult, error) {
	return newCall(c, rpc.LabelTree, "label tree message", "tree result").at(id, sessionID).with(c.callerQuery()).do(ctx, req)
}

// SwitchTree moves a chat to another branch.
func (c *Client) SwitchTree(ctx context.Context, id, sessionID string, req proto.TreeMoveRequest) (proto.TreeResult, error) {
	return newCall(c, rpc.SwitchTree, "switch branch", "tree result").at(id, sessionID).with(c.callerQuery()).do(ctx, req)
}

// NavigateTree moves a chat to another branch, optionally with a summary
// of the branch left behind.
func (c *Client) NavigateTree(ctx context.Context, id, sessionID string, req proto.TreeMoveRequest) (proto.TreeResult, error) {
	return newCall(c, rpc.NavigateTree, "navigate branch", "tree result").at(id, sessionID).with(c.callerQuery()).do(ctx, req)
}

// CopyTree forks or clones a chat into a new one.
func (c *Client) CopyTree(ctx context.Context, id, sessionID string, req proto.TreeCopyRequest) (proto.TreeCopyResult, error) {
	return newCall(c, rpc.CopyTree, "copy branch", "copy result").at(id, sessionID).with(c.callerQuery()).do(ctx, req)
}

// PreviewFiles plans the file restore of a move.
func (c *Client) PreviewFiles(ctx context.Context, id, sessionID string, req proto.FilePreviewRequest) (proto.FilePlan, error) {
	return newCall(c, rpc.PreviewFiles, "preview file restore", "file plan").at(id, sessionID).with(c.callerQuery()).do(ctx, req)
}

// PreviewUndoFiles plans undoing the chat's last file restore.
func (c *Client) PreviewUndoFiles(ctx context.Context, id, sessionID string) (proto.FilePlan, error) {
	return newCall(c, rpc.PreviewUndoFiles, "preview file undo", "file plan").at(id, sessionID).with(c.callerQuery()).get(ctx)
}

// UndoFiles applies a file plan the engine keeps.
func (c *Client) UndoFiles(ctx context.Context, id, sessionID, planID string) (proto.FileRestoreResult, error) {
	return newCall(c, rpc.UndoFiles, "restore files", "file restore result").at(id, sessionID).with(c.callerQuery()).
		do(ctx, proto.FileUndoRequest{PlanID: planID})
}

// CreateBranch branches a chat from highlighted words of a message.
func (c *Client) CreateBranch(ctx context.Context, id, sessionID string, req proto.BranchRequest) (proto.Branch, error) {
	return newCall(c, rpc.CreateBranch, "create branch", "branch").at(id, sessionID).with(c.callerQuery()).do(ctx, req)
}

// ChatLinks lists the links between the workspace's chats.
func (c *Client) ChatLinks(ctx context.Context, id string) ([]proto.ChatLink, error) {
	return newCall(c, rpc.ListChatLinks, "list chat links", "chat links").at(id).get(ctx)
}

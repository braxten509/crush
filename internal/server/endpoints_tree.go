package server

import (
	"errors"
	"net/http"

	"github.com/charmbracelet/crush/internal/agent"
	"github.com/charmbracelet/crush/internal/apigen"
	"github.com/charmbracelet/crush/internal/backend"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/charmbracelet/crush/internal/rpc"
)

// Piece C (tree actions and branch copy, engine-design.md 5.6 and 5.11)
// binds its routes, wraps its events and maps its errors here.

const treeClientDoc = "Client ID (UUID) of the calling app"

// treeEndpoints documents and routes piece C's endpoints.
func (c *controllerV1) treeEndpoints() []apigen.Endpoint {
	return []apigen.Endpoint{
		route(c, rpc.GetTree, c.getTree).
			Summary("Get a chat's tree").
			Description("Returns the chat's revision and one text preview per message. "+
				"Send the revision as expected_revision with every tree change.").
			Tags("tree").
			PathParam("id", "Workspace ID").
			PathParam("sid", "Session ID").
			Fails(404, 500).
			Build(),

		route(c, rpc.GetTreeMessage, c.getTreeMessage).
			Summary("Get one message of a chat's tree").
			Description("Answers 404 when the message is not in the chat.").
			Tags("tree").
			PathParam("id", "Workspace ID").
			PathParam("sid", "Session ID").
			PathParam("mid", "Message ID").
			Fails(404, 500).
			Build(),

		route(c, rpc.LabelTree, c.labelTree).
			Summary("Label a message of a chat's tree").
			Description("An empty label removes it. A stale expected_revision answers 409 revision_conflict.").
			Tags("tree").
			PathParam("id", "Workspace ID").
			PathParam("sid", "Session ID").
			QueryParam("client_id", treeClientDoc).
			Fails(400, 404, 409, 500).
			Build(),

		route(c, rpc.SwitchTree, c.switchTree).
			Summary("Switch a chat to another branch").
			Description("A stale expected_revision answers 409 revision_conflict, and a busy chat "+
				"409 tree_busy; nothing changes then.").
			Tags("tree").
			PathParam("id", "Workspace ID").
			PathParam("sid", "Session ID").
			QueryParam("client_id", treeClientDoc).
			Fails(400, 404, 409, 500).
			Build(),

		route(c, rpc.NavigateTree, c.navigateTree).
			Summary("Navigate a chat to another branch").
			Description("With summarize, the AI writes a summary of the branch left behind. "+
				"A stale expected_revision answers 409 revision_conflict, and a busy chat 409 tree_busy.").
			Tags("tree").
			PathParam("id", "Workspace ID").
			PathParam("sid", "Session ID").
			QueryParam("client_id", treeClientDoc).
			Fails(400, 404, 409, 500).
			Build(),

		route(c, rpc.CopyTree, c.copyTree).
			Summary("Fork or clone a chat").
			Description("Copies the chat into a new one, which every app sees as a created session. "+
				"A stale expected_revision answers 409 revision_conflict, and a busy chat 409 tree_busy.").
			Tags("tree").
			PathParam("id", "Workspace ID").
			PathParam("sid", "Session ID").
			QueryParam("client_id", treeClientDoc).
			Fails(400, 404, 409, 500).
			Build(),

		route(c, rpc.PreviewFiles, c.previewFiles).
			Summary("Preview the file restore of a move").
			Description("The engine keeps the plan for 10 minutes (16 per workspace); apply it "+
				"with files/undo and its plan_id.").
			Tags("tree").
			PathParam("id", "Workspace ID").
			PathParam("sid", "Session ID").
			QueryParam("client_id", treeClientDoc).
			Fails(400, 404, 409, 500).
			Build(),

		route(c, rpc.PreviewUndoFiles, c.previewUndoFiles).
			Summary("Preview undoing the last file restore").
			Description("The engine keeps the plan for 10 minutes (16 per workspace); apply it "+
				"with files/undo and its plan_id.").
			Tags("tree").
			PathParam("id", "Workspace ID").
			PathParam("sid", "Session ID").
			QueryParam("client_id", treeClientDoc).
			Fails(400, 404, 409, 500).
			Build(),

		route(c, rpc.UndoFiles, c.undoFiles).
			Summary("Apply a kept file plan").
			Description("Each plan is used once. An unknown or expired plan_id, or a preview made "+
				"before the chat's newest message, answers 409 plan_expired; a preview made before "+
				"a branch change answers 409 revision_conflict.").
			Tags("tree").
			PathParam("id", "Workspace ID").
			PathParam("sid", "Session ID").
			QueryParam("client_id", treeClientDoc).
			Fails(400, 404, 409, 500).
			Build(),

		route(c, rpc.CreateBranch, c.createBranch).
			Summary("Branch a chat from highlighted words").
			Description("Copies the chat through the message holding the highlight into a new "+
				"top-level chat, even while the AI replies. Offsets are UTF-16 code units into "+
				"the message text. Every app gets the new chat as a created session and its links "+
				"as created chat_link events.").
			Tags("tree").
			PathParam("id", "Workspace ID").
			PathParam("sid", "Session ID").
			QueryParam("client_id", treeClientDoc).
			Fails(400, 404, 409, 500).
			Build(),

		route(c, rpc.ListChatLinks, c.listChatLinks).
			Summary("List the links between chats").
			Description("Origin links nest branches under their parents; the others are links "+
				"copied along with a branch's messages.").
			Tags("tree").
			PathParam("id", "Workspace ID").
			Fails(404, 500).
			Build(),
	}
}

// wrapTreeEvent wraps piece C's fan-in events for SSE. It reports false for
// an event that is not C's. C's events are all engine-sent, so it has none.
func wrapTreeEvent(any) (*pubsub.Payload, bool) { return nil, false }

// treeErrors maps piece C's errors to their statuses and codes.
func treeErrors() []apiError {
	is := func(target error) func(error) bool {
		return func(err error) bool { return errors.Is(err, target) }
	}
	return []apiError{
		{Match: is(agent.ErrTreeRevisionChanged), Status: http.StatusConflict, Code: "revision_conflict"},
		{Match: is(agent.ErrTreeBusy), Status: http.StatusConflict, Code: "tree_busy"},
		{Match: is(message.ErrBadCut), Status: http.StatusBadRequest, Code: "bad_cut"},
		{Match: is(message.ErrCutNotFinished), Status: http.StatusConflict, Code: "cut_not_finished"},
		{Match: is(message.ErrHighlightOverlapsLink), Status: http.StatusConflict, Code: "highlight_overlaps_link"},
		{Match: is(backend.ErrFilePlanExpired), Status: http.StatusConflict, Code: "plan_expired"},
		{Match: is(backend.ErrTreeMessageNotFound), Status: http.StatusNotFound, Code: "not_found"},
	}
}

package backend

import (
	"context"
	"errors"

	"github.com/charmbracelet/crush/internal/db"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/pubsub"
)

// Branch copy over the wire (engine-design.md 5.11).

// chatLinks is the message service's view of the links between chats.
type chatLinks interface {
	ChatLinks(ctx context.Context) ([]db.ChatLink, error)
}

// CreateBranch copies a chat through the highlighted message into a new
// top-level chat. It works while the AI replies in the source chat, and
// leaves that reply alone. Every app then sees the new chat as a created
// session, and its links as created chat links.
func (b *Backend) CreateBranch(ctx context.Context, workspaceID, sessionID string, req proto.BranchRequest) (proto.Branch, error) {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return proto.Branch{}, err
	}
	brancher, ok := ws.Messages.(message.BranchService)
	if !ok {
		return proto.Branch{}, errors.New("branches need the engine's message store")
	}
	res, err := brancher.Branch(ctx, message.BranchRequest{
		SessionID: sessionID, MessageID: req.MessageID,
		HighlightText: req.HighlightText, HighlightStart: req.HighlightStart, HighlightEnd: req.HighlightEnd,
		Title: req.Title,
	})
	if err != nil {
		return proto.Branch{}, err
	}
	if err := announce(ctx, ws, res.SessionID); err != nil {
		return proto.Branch{}, err
	}
	created, err := ws.Sessions.Get(ctx, res.SessionID)
	if err != nil {
		return proto.Branch{}, err
	}
	out := proto.Branch{Session: proto.SessionFromDomain(created), CopiedThrough: res.CopiedThrough, Copied: res.Copied}
	links, err := branchLinks(ctx, ws, res.SessionID)
	if err != nil {
		return proto.Branch{}, err
	}
	for _, link := range links {
		if link.ID == res.LinkID {
			out.Link = link
		}
		b.SendAll(ws.ID, EngineEvent{Type: pubsub.PayloadTypeChatLink, Kind: pubsub.CreatedEvent, Payload: link})
	}
	return out, nil
}

// branchLinks lists the links a branch brought: its origin link, then the
// links copied along with its messages.
func branchLinks(ctx context.Context, ws *Workspace, branchID string) ([]proto.ChatLink, error) {
	store, ok := ws.Messages.(chatLinks)
	if !ok {
		return nil, errors.New("branch storage unavailable")
	}
	all, err := store.ChatLinks(ctx)
	if err != nil {
		return nil, err
	}
	var origin, copied []proto.ChatLink
	for _, l := range all {
		switch {
		case l.SessionID == branchID && l.CopiedFrom == "":
			origin = append(origin, proto.ChatLinkFromDB(l))
		case l.ParentSessionID == branchID && l.CopiedFrom != "":
			copied = append(copied, proto.ChatLinkFromDB(l))
		}
	}
	return append(origin, copied...), nil
}

// ChatLinks lists every link between the workspace's chats, oldest first.
func (b *Backend) ChatLinks(ctx context.Context, workspaceID string) ([]proto.ChatLink, error) {
	ws, err := b.GetWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}
	store, ok := ws.Messages.(chatLinks)
	if !ok {
		return nil, errors.New("branch storage unavailable")
	}
	all, err := store.ChatLinks(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]proto.ChatLink, 0, len(all))
	for _, l := range all {
		out = append(out, proto.ChatLinkFromDB(l))
	}
	return out, nil
}

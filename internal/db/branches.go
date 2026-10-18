package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ChatLink is a link from highlighted words in a message to the branch
// chat they opened (engine-design.md 5.11). SessionID is the branch the
// link opens; ParentSessionID is the chat that shows the link.
type ChatLink struct {
	ID, SessionID, ParentSessionID, SourceMessageID, HighlightText string
	HighlightStart, HighlightEnd                                   int
	CopiedFrom                                                     string // "" for the origin link
	CreatedAt                                                      int64
}

// BranchCopy describes a branch copy: the cut of the source chat and the
// branch's origin link.
type BranchCopy struct {
	Source     string   // source chat
	MessageIDs []string // the cut: ancestry from the root through the last copied message, in order
	NewID      string
	Title      string
	Origin     ChatLink // filled except ID, SessionID and CreatedAt
}

// CopyBranch copies MessageIDs into a new top-level chat in one transaction,
// with the extras listed below, and inserts the origin link. It returns the
// map from old to new message IDs.
//
// The extras are the ones every copy brings (copyMessages and
// copyChatExtras). The source chat's rows, head and revision are only read.
func (q *Queries) CopyBranch(ctx context.Context, c BranchCopy) (map[string]string, error) {
	conn, ok := q.db.(*sql.DB)
	if !ok {
		return nil, fmt.Errorf("branch copy requires a database connection")
	}
	if len(c.MessageIDs) == 0 {
		return nil, fmt.Errorf("a branch copies at least one message")
	}
	ids, err := json.Marshal(c.MessageIDs)
	if err != nil {
		return nil, err
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var owned int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM json_each(?) j JOIN messages m ON m.id=j.value WHERE m.session_id=?`, string(ids), c.Source).Scan(&owned); err != nil {
		return nil, err
	}
	if owned != len(c.MessageIDs) {
		return nil, fmt.Errorf("the cut holds messages of another chat")
	}
	if _, err := New(tx).CreateSession(ctx, CreateSessionParams{ID: c.NewID, Title: c.Title}); err != nil {
		return nil, err
	}
	copied, err := copyMessages(ctx, tx, c.NewID, c.MessageIDs)
	if err != nil {
		return nil, err
	}
	if err := copyChatExtras(ctx, tx, c.Source, c.NewID); err != nil {
		return nil, err
	}
	if err := pointAtSummary(ctx, tx, c.NewID); err != nil {
		return nil, err
	}
	origin := c.Origin
	origin.ID, origin.SessionID, origin.CopiedFrom, origin.CreatedAt = uuid.NewString(), c.NewID, "", time.Now().Unix()
	if origin.ParentSessionID == "" {
		origin.ParentSessionID = c.Source
	}
	if err := insertLink(ctx, tx, origin); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return copied, nil
}

func insertLink(ctx context.Context, q DBTX, l ChatLink) error {
	_, err := q.ExecContext(ctx, `INSERT INTO chat_branches(id,session_id,parent_session_id,source_message_id,highlight_text,highlight_start,highlight_end,copied_from,created_at)
 VALUES(?,?,?,?,?,?,?,?,?)`, l.ID, l.SessionID, l.ParentSessionID, l.SourceMessageID, l.HighlightText, l.HighlightStart, l.HighlightEnd,
		sql.NullString{String: l.CopiedFrom, Valid: l.CopiedFrom != ""}, l.CreatedAt)
	return err
}

const chatLinkColumns = `id,session_id,parent_session_id,source_message_id,highlight_text,highlight_start,highlight_end,coalesce(copied_from,''),created_at`

// ChatLinks lists every link in the database, oldest first.
func (q *Queries) ChatLinks(ctx context.Context) ([]ChatLink, error) {
	return q.chatLinks(ctx, `SELECT `+chatLinkColumns+` FROM chat_branches ORDER BY created_at,rowid`)
}

// BranchOrigin returns the origin link of the branch chat sessionID. ok is
// false when the chat is not a branch, or no longer has its parent.
func (q *Queries) BranchOrigin(ctx context.Context, sessionID string) (ChatLink, bool, error) {
	links, err := q.chatLinks(ctx, `SELECT `+chatLinkColumns+` FROM chat_branches WHERE session_id=? AND copied_from IS NULL`, sessionID)
	if err != nil || len(links) == 0 {
		return ChatLink{}, false, err
	}
	return links[0], true, nil
}

// LinksOnMessage lists the links shown on a message, in text order.
func (q *Queries) LinksOnMessage(ctx context.Context, messageID string) ([]ChatLink, error) {
	return q.chatLinks(ctx, `SELECT `+chatLinkColumns+` FROM chat_branches WHERE source_message_id=? ORDER BY highlight_start,created_at,rowid`, messageID)
}

func (q *Queries) chatLinks(ctx context.Context, query string, args ...any) ([]ChatLink, error) {
	rows, err := q.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var links []ChatLink
	for rows.Next() {
		var l ChatLink
		if err := rows.Scan(&l.ID, &l.SessionID, &l.ParentSessionID, &l.SourceMessageID, &l.HighlightText, &l.HighlightStart, &l.HighlightEnd, &l.CopiedFrom, &l.CreatedAt); err != nil {
			return nil, err
		}
		links = append(links, l)
	}
	return links, rows.Err()
}

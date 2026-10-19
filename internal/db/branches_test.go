package db_test

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/crush/internal/db"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/stretchr/testify/require"
)

// branchStore is a migrated database with the real message service on top,
// so branches are cut the way the engine cuts them.
type branchStore struct {
	conn *sql.DB
	q    *db.Queries
	msgs message.Service
}

func newBranchStore(t *testing.T) branchStore {
	t.Helper()
	dir := t.TempDir()
	conn, err := db.Connect(context.Background(), dir)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Release(dir) })
	q := db.New(conn)
	return branchStore{conn: conn, q: q, msgs: message.NewService(q, message.WithDebounce(0))}
}

func (s branchStore) chat(t *testing.T, id, title string) {
	t.Helper()
	_, err := s.q.CreateSession(t.Context(), db.CreateSessionParams{ID: id, Title: title})
	require.NoError(t, err)
}

func (s branchStore) add(t *testing.T, sessionID string, role message.MessageRole, parts ...message.ContentPart) message.Message {
	t.Helper()
	m, err := s.msgs.Create(t.Context(), sessionID, message.CreateMessageParams{Role: role, Parts: parts, Model: "model", Provider: "provider"})
	require.NoError(t, err)
	return m
}

func (s branchStore) exec(t *testing.T, query string, args ...any) {
	t.Helper()
	_, err := s.conn.ExecContext(t.Context(), query, args...)
	require.NoError(t, err)
}

// toolChat is a chat whose second message ran two tools:
// u1, a1 (text and two tool calls), t1 and t2 (their results), a2, u2.
type toolChat struct{ u1, a1, t1, t2, a2, u2 message.Message }

func (s branchStore) toolChat(t *testing.T, sessionID string) toolChat {
	t.Helper()
	s.chat(t, sessionID, "Trip")
	var c toolChat
	c.u1 = s.add(t, sessionID, message.User, message.TextContent{Text: "Plan the trip"})
	c.a1 = s.add(t, sessionID, message.Assistant,
		message.TextContent{Text: "Let me look at the map first."},
		message.ToolCall{ID: "c1", Name: "bash", Input: `{"command":"ls"}`, Finished: true},
		message.ToolCall{ID: "c2", Name: "view", Input: `{"file_path":"map.txt"}`, Finished: true},
		message.Finish{Reason: message.FinishReasonToolUse})
	c.t1 = s.add(t, sessionID, message.Tool, message.ToolResult{ToolCallID: "c1", Name: "bash", Content: "map.txt"})
	c.t2 = s.add(t, sessionID, message.Tool, message.ToolResult{ToolCallID: "c2", Name: "view", Content: "a map"})
	c.a2 = s.add(t, sessionID, message.Assistant, message.TextContent{Text: "Here is the plan."}, message.Finish{Reason: message.FinishReasonEndTurn})
	c.u2 = s.add(t, sessionID, message.User, message.TextContent{Text: "Thanks"})
	return c
}

// branch makes a branch from the first place word appears in m's text.
func (s branchStore) branch(t *testing.T, m message.Message, word, title string) message.BranchResult {
	t.Helper()
	start := strings.Index(m.Content().Text, word)
	require.GreaterOrEqual(t, start, 0, "%q is not in the message", word)
	res, err := s.msgs.(message.BranchService).Branch(t.Context(), message.BranchRequest{
		SessionID: m.SessionID, MessageID: m.ID,
		HighlightText: word, HighlightStart: start, HighlightEnd: start + len(word),
		Title: title,
	})
	require.NoError(t, err)
	return res
}

// path is a chat's messages along its current branch, oldest first.
func (s branchStore) path(t *testing.T, sessionID string) []message.Message {
	t.Helper()
	ids, err := s.q.TreePath(t.Context(), sessionID)
	require.NoError(t, err)
	out := make([]message.Message, 0, len(ids))
	for _, id := range ids {
		m, err := s.msgs.Get(t.Context(), id)
		require.NoError(t, err)
		out = append(out, m)
	}
	return out
}

// sameMessages checks that copies hold the originals' role and parts, in
// order, under new IDs.
func sameMessages(t *testing.T, want, got []message.Message) {
	t.Helper()
	require.Len(t, got, len(want))
	for i := range want {
		require.NotEqual(t, want[i].ID, got[i].ID)
		require.Equal(t, want[i].Role, got[i].Role, "message %d", i)
		require.Equal(t, want[i].Content().Text, got[i].Content().Text, "message %d", i)
		require.Equal(t, want[i].ToolCalls(), got[i].ToolCalls(), "message %d", i)
		require.Equal(t, want[i].ToolResults(), got[i].ToolResults(), "message %d", i)
	}
}

func TestCopyBranchThroughCutWithToolResults(t *testing.T) {
	s := newBranchStore(t)
	c := s.toolChat(t, "src")

	// An assistant reply that ran tools brings the results of its calls,
	// and stops before the next reply.
	res := s.branch(t, c.a1, "map", "Maps")
	sameMessages(t, []message.Message{c.u1, c.a1, c.t1, c.t2}, s.path(t, res.SessionID))
	require.Equal(t, c.t2.ID, res.CopiedThrough)
	require.Equal(t, 4, res.Copied)
	all, err := s.q.ListMessagesBySession(t.Context(), res.SessionID)
	require.NoError(t, err)
	require.Len(t, all, 4, "nothing past the cut is copied")

	// A reply without tools ends the branch, and so does a user message.
	res = s.branch(t, c.a2, "plan", "Plan")
	sameMessages(t, []message.Message{c.u1, c.a1, c.t1, c.t2, c.a2}, s.path(t, res.SessionID))
	res = s.branch(t, c.u2, "Thanks", "Thanks")
	sameMessages(t, []message.Message{c.u1, c.a1, c.t1, c.t2, c.a2, c.u2}, s.path(t, res.SessionID))

	// A tool call answered twice (the second answer on another branch)
	// takes the newer answer.
	s.chat(t, "again", "Again")
	u := s.add(t, "again", message.User, message.TextContent{Text: "Look"})
	a := s.add(t, "again", message.Assistant, message.TextContent{Text: "Looking now."},
		message.ToolCall{ID: "k1", Name: "bash", Input: `{"command":"ls"}`, Finished: true},
		message.Finish{Reason: message.FinishReasonToolUse})
	s.add(t, "again", message.Tool, message.ToolResult{ToolCallID: "k1", Name: "bash", Content: "old"})
	require.NoError(t, s.q.SwitchTree(t.Context(), "again", a.ID))
	newer := s.add(t, "again", message.Tool, message.ToolResult{ToolCallID: "k1", Name: "bash", Content: "new"})
	res = s.branch(t, a, "Looking", "Looking")
	sameMessages(t, []message.Message{u, a, newer}, s.path(t, res.SessionID))
}

// seedExtras gives the chat one of everything a copy brings along, and a
// branch from c.u1 whose link a later copy of u1 must bring.
func (s branchStore) seedExtras(t *testing.T, sessionID string, c toolChat) message.BranchResult {
	t.Helper()
	s.exec(t, `INSERT INTO message_review_details(message_id,payload) VALUES(?,?)`, c.t1.ID, []byte("review of t1"))
	s.exec(t, `INSERT INTO session_kept_skills(session_id,skill_id,name,position,turned_off,by_agent) VALUES(?,?,?,?,?,?),(?,?,?,?,?,?)`,
		sessionID, "go", "Go", 0, 0, 1, sessionID, "rust", "Rust", 1, 1, 0)
	s.exec(t, `INSERT INTO session_context_budgets(session_id,provider,model,token_limit,estimated) VALUES(?,?,?,?,?)`, sessionID, "anthropic", "claude", 200000, 1)
	s.exec(t, `INSERT INTO read_files(session_id,path,read_at) VALUES(?,?,?),(?,?,?)`, sessionID, "/work/a.go", 100, sessionID, "/work/b.go", 200)
	s.exec(t, `UPDATE sessions SET todos=?, cost=?, prompt_tokens=?, completion_tokens=? WHERE id=?`,
		`[{"content":"pack","status":"pending","active_form":"Packing"}]`, 2.5, 100, 50, sessionID)
	s.exec(t, `UPDATE messages SET prism_model_id=?, prism_model_name=?, prism_hypercredit_savings=?, prism_dollar_savings=? WHERE id=?`,
		"prism-1", "Prism One", 1.5, 0.25, c.a1.ID)
	require.NoError(t, s.q.RecordMarker(t.Context(), db.MessageMarker{MessageID: c.a1.ID, SessionID: sessionID, CLI: "claude-code", NativeSession: "native-1", Marker: "uuid-a1", MarkerKind: "message"}))
	require.NoError(t, s.q.RecordMarker(t.Context(), db.MessageMarker{MessageID: c.t2.ID, SessionID: sessionID, CLI: "claude-code", NativeSession: "native-1", Marker: "uuid-t2", MarkerKind: "message"}))
	return s.branch(t, c.u1, "trip", "Trip ideas")
}

// checkExtras checks that the copy newID holds the extras seedExtras gave
// its source, on its own copies of u1, a1, t1 and t2 (path[0:4]), and that
// its cost and token totals start at 0.
func (s branchStore) checkExtras(t *testing.T, newID string, sibling message.BranchResult) {
	t.Helper()
	ctx := t.Context()
	path := s.path(t, newID)
	require.GreaterOrEqual(t, len(path), 4)
	u1, a1, t1, t2 := path[0], path[1], path[2], path[3]

	var payload []byte
	require.NoError(t, s.conn.QueryRowContext(ctx, `SELECT payload FROM message_review_details WHERE message_id=?`, t1.ID).Scan(&payload))
	require.Equal(t, "review of t1", string(payload))

	type kept struct {
		id, name               string
		position, off, byAgent int
	}
	rows, err := s.conn.QueryContext(ctx, `SELECT skill_id,name,position,turned_off,by_agent FROM session_kept_skills WHERE session_id=? ORDER BY position`, newID)
	require.NoError(t, err)
	var skills []kept
	for rows.Next() {
		var k kept
		require.NoError(t, rows.Scan(&k.id, &k.name, &k.position, &k.off, &k.byAgent))
		skills = append(skills, k)
	}
	require.NoError(t, rows.Close())
	require.Equal(t, []kept{{"go", "Go", 0, 0, 1}, {"rust", "Rust", 1, 1, 0}}, skills)

	var provider, model string
	var limit, estimated int
	require.NoError(t, s.conn.QueryRowContext(ctx, `SELECT provider,model,token_limit,estimated FROM session_context_budgets WHERE session_id=?`, newID).Scan(&provider, &model, &limit, &estimated))
	require.Equal(t, []any{"anthropic", "claude", 200000, 1}, []any{provider, model, limit, estimated})

	var reads int
	require.NoError(t, s.conn.QueryRowContext(ctx, `SELECT count(*) FROM read_files WHERE session_id=? AND ((path='/work/a.go' AND read_at=100) OR (path='/work/b.go' AND read_at=200))`, newID).Scan(&reads))
	require.Equal(t, 2, reads)

	copied, err := s.q.GetSessionByID(ctx, newID)
	require.NoError(t, err)
	require.Equal(t, `[{"content":"pack","status":"pending","active_form":"Packing"}]`, copied.Todos.String)
	require.Zero(t, copied.Cost)
	require.Zero(t, copied.PromptTokens)
	require.Zero(t, copied.CompletionTokens)
	require.False(t, copied.ParentSessionID.Valid && copied.ParentSessionID.String != "")

	var prismID, prismName string
	var credits, dollars float64
	require.NoError(t, s.conn.QueryRowContext(ctx, `SELECT prism_model_id,prism_model_name,prism_hypercredit_savings,prism_dollar_savings FROM messages WHERE id=?`, a1.ID).Scan(&prismID, &prismName, &credits, &dollars))
	require.Equal(t, []any{"prism-1", "Prism One", 1.5, 0.25}, []any{prismID, prismName, credits, dollars})

	markers, err := s.q.MarkersForSession(ctx, newID)
	require.NoError(t, err)
	require.Len(t, markers, 2)
	byMessage := map[string]db.MessageMarker{}
	for _, m := range markers {
		byMessage[m.MessageID] = m
	}
	require.Equal(t, "uuid-a1", byMessage[a1.ID].Marker)
	require.Equal(t, "uuid-t2", byMessage[t2.ID].Marker)
	require.Equal(t, "native-1", byMessage[a1.ID].NativeSession)
	require.Equal(t, "claude-code", byMessage[t2.ID].CLI)

	links, err := s.q.LinksOnMessage(ctx, u1.ID)
	require.NoError(t, err)
	require.Len(t, links, 1, "the sibling branch's link comes along")
	require.Equal(t, sibling.SessionID, links[0].SessionID)
	require.Equal(t, newID, links[0].ParentSessionID)
	require.Equal(t, sibling.LinkID, links[0].CopiedFrom)
	require.Equal(t, "trip", links[0].HighlightText)
}

func TestCopyBranchCopiesExtras(t *testing.T) {
	s := newBranchStore(t)
	c := s.toolChat(t, "src")
	sibling := s.seedExtras(t, "src", c)
	res := s.branch(t, c.a1, "map", "Maps")
	s.checkExtras(t, res.SessionID, sibling)
}

// dump renders a chat's own rows: everything a copy could touch. A
// branch's origin link names its parent, but is the branch's row.
func (s branchStore) dump(t *testing.T, sessionID string) string {
	t.Helper()
	var b strings.Builder
	for _, query := range []string{
		`SELECT * FROM sessions WHERE id=?`,
		`SELECT * FROM messages WHERE session_id=? ORDER BY rowid`,
		`SELECT * FROM tree_nodes WHERE session_id=? ORDER BY rowid`,
		`SELECT * FROM tree_heads WHERE session_id=?`,
		`SELECT * FROM session_kept_skills WHERE session_id=? ORDER BY rowid`,
		`SELECT * FROM session_context_budgets WHERE session_id=?`,
		`SELECT * FROM read_files WHERE session_id=? ORDER BY rowid`,
		`SELECT * FROM cli_message_markers WHERE session_id=? ORDER BY rowid`,
		`SELECT * FROM file_history_points WHERE session_id=? ORDER BY rowid`,
		`SELECT * FROM file_history_changes WHERE session_id=? ORDER BY rowid`,
		`SELECT r.* FROM message_review_details r JOIN messages m ON m.id=r.message_id WHERE m.session_id=? ORDER BY r.rowid`,
		`SELECT s.* FROM tree_branch_summaries s JOIN messages m ON m.id=s.message_id WHERE m.session_id=? ORDER BY s.rowid`,
		`SELECT * FROM chat_branches WHERE session_id=? OR (parent_session_id=? AND copied_from IS NOT NULL) ORDER BY rowid`,
	} {
		args := make([]any, strings.Count(query, "?"))
		for i := range args {
			args[i] = sessionID
		}
		rows, err := s.conn.QueryContext(t.Context(), query, args...)
		require.NoError(t, err)
		columns, err := rows.Columns()
		require.NoError(t, err)
		fmt.Fprintf(&b, "%s\n", query)
		for rows.Next() {
			values := make([]any, len(columns))
			pointers := make([]any, len(columns))
			for i := range values {
				pointers[i] = &values[i]
			}
			require.NoError(t, rows.Scan(pointers...))
			fmt.Fprintf(&b, "%#v\n", values)
		}
		require.NoError(t, rows.Err())
		require.NoError(t, rows.Close())
	}
	return b.String()
}

func TestCopyBranchLeavesSourceUntouched(t *testing.T) {
	s := newBranchStore(t)
	c := s.toolChat(t, "src")
	s.seedExtras(t, "src", c)
	revision, err := s.q.TreeRevision(t.Context(), "src")
	require.NoError(t, err)
	origins := func() int {
		var n int
		require.NoError(t, s.conn.QueryRowContext(t.Context(), `SELECT count(*) FROM chat_branches WHERE parent_session_id='src' AND copied_from IS NULL`).Scan(&n))
		return n
	}
	require.Equal(t, 1, origins())

	before := s.dump(t, "src")
	require.Contains(t, before, "uuid-a1", "the dump holds the source's extras")
	s.branch(t, c.a1, "map", "Maps")
	s.branch(t, c.u2, "Thanks", "Thanks")
	require.Equal(t, before, s.dump(t, "src"))
	after, err := s.q.TreeRevision(t.Context(), "src")
	require.NoError(t, err)
	require.Equal(t, revision, after)
	// The only new rows naming the source are the branches' origin links.
	require.Equal(t, 3, origins())
}

func TestCopyBranchLinksComeAlong(t *testing.T) {
	s := newBranchStore(t)
	c := s.toolChat(t, "src")
	sibling := s.branch(t, c.u1, "trip", "Trip ideas")
	second := s.branch(t, c.a1, "map", "Maps")

	links, err := s.q.ChatLinks(t.Context())
	require.NoError(t, err)
	var origins, copies []db.ChatLink
	for _, l := range links {
		if l.CopiedFrom == "" {
			origins = append(origins, l)
		} else {
			copies = append(copies, l)
		}
	}
	require.Len(t, origins, 2)
	require.Len(t, copies, 1)

	// The second branch carries the first branch's link on its copy of u1.
	path := s.path(t, second.SessionID)
	require.Equal(t, sibling.SessionID, copies[0].SessionID)
	require.Equal(t, second.SessionID, copies[0].ParentSessionID)
	require.Equal(t, path[0].ID, copies[0].SourceMessageID)
	require.Equal(t, sibling.LinkID, copies[0].CopiedFrom)

	// Each branch has one origin: its parent chat and source message.
	for _, b := range []struct {
		res    message.BranchResult
		source message.Message
	}{{sibling, c.u1}, {second, c.a1}} {
		origin, ok, err := s.q.BranchOrigin(t.Context(), b.res.SessionID)
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, b.res.LinkID, origin.ID)
		require.Equal(t, "src", origin.ParentSessionID)
		require.Equal(t, b.source.ID, origin.SourceMessageID)
	}
	_, isBranch, err := s.q.BranchOrigin(t.Context(), "src")
	require.NoError(t, err)
	require.False(t, isBranch)

	// A branch of the branch brings the copied link along again.
	third := s.branch(t, path[0], "Plan", "Planning")
	thirdPath := s.path(t, third.SessionID)
	again, err := s.q.LinksOnMessage(t.Context(), thirdPath[0].ID)
	require.NoError(t, err)
	require.Len(t, again, 1)
	require.Equal(t, sibling.SessionID, again[0].SessionID)
	require.Equal(t, copies[0].ID, again[0].CopiedFrom)
	origin, ok, err := s.q.BranchOrigin(t.Context(), third.SessionID)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, second.SessionID, origin.ParentSessionID)

	// A second origin for the same branch is refused by the schema.
	_, err = s.conn.ExecContext(t.Context(), `INSERT INTO chat_branches(id,session_id,parent_session_id,source_message_id,highlight_text,highlight_start,highlight_end,created_at) VALUES('x',?,?,?,'w',0,1,1)`,
		second.SessionID, "src", c.u1.ID)
	require.Error(t, err)
}

// The copy checks for overlapping links in its own transaction: a link that
// lands after Branch's other checks still refuses it, and the refused copy
// writes nothing.
func TestCopyBranchRefusesOverlapInItsTransaction(t *testing.T) {
	s := newBranchStore(t)
	c := s.toolChat(t, "src")
	first := s.branch(t, c.a2, "the plan", "Plan")
	path, err := s.q.TreeAncestry(t.Context(), "src", c.a2.ID)
	require.NoError(t, err)

	_, err = s.q.CopyBranch(t.Context(), db.BranchCopy{
		Source: "src", MessageIDs: path, NewID: "late", Title: "Late",
		Origin: db.ChatLink{SourceMessageID: c.a2.ID, HighlightText: "plan", HighlightStart: 12, HighlightEnd: 16},
	})
	require.ErrorIs(t, err, db.ErrHighlightOverlapsLink)
	_, err = s.q.GetSessionByID(t.Context(), "late")
	require.ErrorIs(t, err, sql.ErrNoRows, "the refused copy made no chat")
	links, err := s.q.LinksOnMessage(t.Context(), c.a2.ID)
	require.NoError(t, err)
	require.Len(t, links, 1)
	require.Equal(t, first.LinkID, links[0].ID)
}

func TestDeletingParentMakesBranchTopLevel(t *testing.T) {
	s := newBranchStore(t)
	c := s.toolChat(t, "src")
	res := s.branch(t, c.a1, "map", "Maps")

	require.NoError(t, session.NewService(s.q, s.conn).Delete(t.Context(), "src"))

	_, ok, err := s.q.BranchOrigin(t.Context(), res.SessionID)
	require.NoError(t, err)
	require.False(t, ok, "the origin link goes with its parent")
	links, err := s.q.ChatLinks(t.Context())
	require.NoError(t, err)
	require.Empty(t, links)

	branch, err := s.q.GetSessionByID(t.Context(), res.SessionID)
	require.NoError(t, err)
	require.Equal(t, "Maps", branch.Title)
	require.False(t, branch.ParentSessionID.Valid && branch.ParentSessionID.String != "")
	require.Len(t, s.path(t, res.SessionID), 4, "the branch keeps its messages")
}

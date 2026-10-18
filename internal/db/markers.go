package db

import (
	"context"
	"time"
)

// MessageMarker is an AI tool's own ID for a Crush message, recorded as the
// reply arrives, so a branch can fork the tool's memory at its cut
// (engine-design.md 5.11).
type MessageMarker struct {
	MessageID, SessionID, CLI, NativeSession, Marker, MarkerKind string
	CreatedAt                                                    int64
}

// RecordMarker saves a marker. A newer marker of the same tool for the same
// message replaces the older one. A zero CreatedAt means now.
func (q *Queries) RecordMarker(ctx context.Context, m MessageMarker) error {
	if m.CreatedAt == 0 {
		m.CreatedAt = time.Now().Unix()
	}
	_, err := q.db.ExecContext(ctx, `INSERT OR REPLACE INTO cli_message_markers(message_id,session_id,cli,native_session,marker,marker_kind,created_at)
 VALUES(?,?,?,?,?,?,?)`, m.MessageID, m.SessionID, m.CLI, m.NativeSession, m.Marker, m.MarkerKind, m.CreatedAt)
	return err
}

// MarkersForSession lists a chat's markers, oldest first.
func (q *Queries) MarkersForSession(ctx context.Context, sessionID string) ([]MessageMarker, error) {
	rows, err := q.db.QueryContext(ctx, `SELECT message_id,session_id,cli,native_session,marker,marker_kind,created_at
 FROM cli_message_markers WHERE session_id=? ORDER BY created_at,rowid`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var markers []MessageMarker
	for rows.Next() {
		var m MessageMarker
		if err := rows.Scan(&m.MessageID, &m.SessionID, &m.CLI, &m.NativeSession, &m.Marker, &m.MarkerKind, &m.CreatedAt); err != nil {
			return nil, err
		}
		markers = append(markers, m)
	}
	return markers, rows.Err()
}

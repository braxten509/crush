package sessioncatalog

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/charmbracelet/crush/internal/db"
	"github.com/charmbracelet/crush/internal/projects"
	"github.com/charmbracelet/crush/internal/session"
)

// Catalog uses the registered project locations, never a scan of the home folder.
type Catalog struct{ Directory, DataDirectory string }

func (c Catalog) locations() ([]projects.Project, error) {
	known, err := projects.List()
	if err != nil {
		return nil, err
	}
	return append([]projects.Project{{Path: c.Directory, DataDir: c.DataDirectory}}, known...), nil
}

func catalogPath(path string) string {
	full, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	if real, err := filepath.EvalSymlinks(full); err == nil {
		return real
	}
	return full
}

// List omits drafts with no user message. Old abandoned drafts are discarded;
// the selected chat and recently created drafts are protected while a first
// message may be in flight. The DELETE rechecks message existence atomically.
func (c Catalog) List(ctx context.Context, selected string) ([]session.Session, error) {
	locations, err := c.locations()
	if err != nil {
		return nil, err
	}
	result := []session.Session{}
	seen := map[string]bool{}
	for _, project := range locations {
		if project.DataDir == "" {
			continue
		}
		data := catalogPath(project.DataDir)
		if seen[data] {
			continue
		}
		seen[data] = true
		if _, err := os.Stat(filepath.Join(data, "crush.db")); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return nil, err
		}
		entries, err := catalogProject(ctx, project.Path, data, selected)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, fmt.Errorf("read saved chats in %s: %w", project.Path, err)
		}
		result = append(result, entries...)
	}
	slices.SortStableFunc(result, func(a, b session.Session) int {
		if a.UpdatedAt > b.UpdatedAt {
			return -1
		}
		if a.UpdatedAt < b.UpdatedAt {
			return 1
		}
		return 0
	})
	return result, nil
}

func catalogProject(ctx context.Context, directory, data, selected string) ([]session.Session, error) {
	conn, err := db.Connect(ctx, data)
	if err != nil {
		return nil, err
	}
	defer db.Release(data)
	if err := discardEmptyBefore(ctx, conn, time.Now().Add(-5*time.Minute).Unix(), selected); err != nil {
		slog.Warn("Could not discard abandoned empty chats", "directory", directory, "error", err)
	}
	rows, err := conn.QueryContext(ctx, `SELECT s.id, s.title, s.message_count, s.created_at, s.updated_at
 FROM sessions s WHERE s.parent_session_id IS NULL
 AND EXISTS (SELECT 1 FROM messages m WHERE m.session_id=s.id AND m.role='user')
 ORDER BY s.updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []session.Session{}
	for rows.Next() {
		s := session.Session{Directory: directory, DataDirectory: data}
		if err := rows.Scan(&s.ID, &s.Title, &s.MessageCount, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, err
		}
		result = append(result, s)
	}
	return result, rows.Err()
}

func discardEmptyBefore(ctx context.Context, conn *sql.DB, before int64, selected string) error {
	_, err := conn.ExecContext(ctx, `DELETE FROM sessions WHERE parent_session_id IS NULL
 AND id != ? AND created_at < ?
 AND NOT EXISTS (SELECT 1 FROM messages WHERE session_id=sessions.id AND role='user')
 AND NOT EXISTS (SELECT 1 FROM messages WHERE session_id=sessions.id AND role='assistant' AND finished_at IS NULL)
 AND NOT EXISTS (SELECT 1 FROM sessions child WHERE child.parent_session_id=sessions.id)`, selected, before)
	return err
}

// Change validates the source against the registry so a client cannot supply
// an arbitrary database path. Renaming changes only the title, not usage data.
func (c Catalog) Change(ctx context.Context, target session.Session, remove bool) error {
	locations, err := c.locations()
	if err != nil {
		return err
	}
	for _, project := range locations {
		if project.DataDir == "" || catalogPath(project.DataDir) != catalogPath(target.DataDirectory) || catalogPath(project.Path) != catalogPath(target.Directory) {
			continue
		}
		if _, err := os.Stat(filepath.Join(project.DataDir, "crush.db")); err != nil {
			return err
		}
		conn, err := db.Connect(ctx, project.DataDir)
		if err != nil {
			return err
		}
		defer db.Release(project.DataDir)
		if !remove {
			_, err = conn.ExecContext(ctx, "UPDATE sessions SET title=? WHERE id=?", target.Title, target.ID)
			return err
		}
		// Do not delete a conversation currently streaming in another window.
		var unfinished bool
		if err := conn.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM messages WHERE session_id=? AND role='assistant' AND finished_at IS NULL)", target.ID).Scan(&unfinished); err != nil {
			return err
		}
		if unfinished {
			return fmt.Errorf("this chat is still running")
		}
		service := session.NewService(db.New(conn), conn)
		return service.Delete(ctx, target.ID)
	}
	return fmt.Errorf("the saved chat's folder is no longer registered")
}

// DiscardEmpty drops a draft explicitly abandoned by its owner. A user message,
// including an attachment-only message, always protects the conversation.
func (c Catalog) DiscardEmpty(ctx context.Context, id string) error {
	if id == "" {
		return nil
	}
	conn, err := db.Connect(ctx, c.DataDirectory)
	if err != nil {
		return err
	}
	defer db.Release(c.DataDirectory)
	_, err = conn.ExecContext(ctx, `DELETE FROM sessions WHERE id=? AND parent_session_id IS NULL
 AND NOT EXISTS(SELECT 1 FROM messages WHERE session_id=sessions.id AND role='user')
 AND NOT EXISTS(SELECT 1 FROM messages WHERE session_id=sessions.id AND role='assistant' AND finished_at IS NULL)
 AND NOT EXISTS(SELECT 1 FROM sessions child WHERE child.parent_session_id=sessions.id)`, id)
	return err
}

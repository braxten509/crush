// The shared index contains only the project database path and session key.
// SQLite assigns the public numbers under its cross-process writer lock.
package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/charmbracelet/crush/internal/home"
)

func numberIndex() string { return filepath.Join(home.CrushData(), "chat-numbers.db") }

func AssignChatNumber(conn *sql.DB, id string) (int64, error) {
	var path string
	if err := conn.QueryRow("SELECT file FROM pragma_database_list WHERE name='main'").Scan(&path); err != nil || path == "" {
		return 0, err
	}
	if err := os.MkdirAll(home.CrushData(), 0700); err != nil {
		return 0, err
	}
	index, err := OpenForeign(numberIndex())
	if err != nil {
		return 0, err
	}
	defer index.Close()
	if _, err = index.Exec(`CREATE TABLE IF NOT EXISTS chats(number INTEGER PRIMARY KEY, path TEXT NOT NULL, session TEXT NOT NULL, UNIQUE(path,session))`); err != nil {
		return 0, err
	}
	if _, err = index.Exec("INSERT INTO chats(path,session) VALUES(?,?) ON CONFLICT DO NOTHING", path, id); err != nil {
		return 0, err
	}
	var number int64
	err = index.QueryRow("SELECT number FROM chats WHERE path=? AND session=?", path, id).Scan(&number)
	return number, err
}

func ResolveChatNumber(ctx context.Context, number int64) (path, id string, err error) {
	index, err := ConnectReadOnly(ctx, numberIndex())
	if err != nil {
		return "", "", err
	}
	defer index.Close()
	err = index.QueryRowContext(ctx, "SELECT path,session FROM chats WHERE number=?", number).Scan(&path, &id)
	if errors.Is(err, sql.ErrNoRows) {
		err = fmt.Errorf("unknown chat number %d", number)
	}
	return
}

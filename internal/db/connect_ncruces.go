//go:build !((darwin && (amd64 || arm64)) || (freebsd && (amd64 || arm64)) || (linux && (386 || amd64 || arm || arm64 || loong64 || ppc64le || riscv64 || s390x)) || (windows && (386 || amd64 || arm64)))

package db

import (
	"database/sql"
	"fmt"
	"net/url"
	"path/filepath"

	"github.com/ncruces/go-sqlite3"
	"github.com/ncruces/go-sqlite3/driver"
)

func openDBReadOnly(dbPath string) (*sql.DB, error) {
	absolute, err := filepath.Abs(dbPath)
	if err != nil {
		return nil, err
	}
	dsn := (&url.URL{Scheme: "file", Path: absolute, RawQuery: "mode=ro&_txlock=immediate"}).String()
	db, err := driver.Open(dsn, registerTreeReader)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	return db, nil
}

func openDB(dbPath string) (*sql.DB, error) {
	// Use BEGIN IMMEDIATE so writers acquire the reserved lock up front,
	// preventing deferred-to-writer upgrade deadlocks. The "file:" prefix
	// is required for the ncruces driver to parse query parameters.
	absolute, err := filepath.Abs(dbPath)
	if err != nil {
		return nil, err
	}
	dsn := (&url.URL{Scheme: "file", Path: absolute, RawQuery: "_txlock=immediate"}).String()
	db, err := driver.Open(dsn, func(c *sqlite3.Conn) error {
		if err := registerTreeReader(c); err != nil {
			return err
		}
		// Set pragmas for better performance via _pragma query params.
		// Format: PRAGMA name = value;
		for name, value := range pragmas {
			if err := c.Exec(fmt.Sprintf("PRAGMA %s = %s;", name, value)); err != nil {
				return fmt.Errorf("failed to set pragma %q: %w", name, err)
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	return db, nil
}

func registerTreeReader(c *sqlite3.Conn) error {
	return c.CreateFunction("Upgrade Crush: this database uses session trees", 0, sqlite3.DETERMINISTIC, func(ctx sqlite3.Context, args ...sqlite3.Value) { ctx.ResultInt(1) })
}

// OpenForeign opens another program's SQLite file (an agent CLI's store)
// with secure_delete on, so removed rows leave no readable text, and no
// migrations. Locked chats move rows out of these (owner, 2026-11-03).
func OpenForeign(path string) (*sql.DB, error) {
	dsn := (&url.URL{Scheme: "file", Path: path, RawQuery: "_txlock=immediate"}).String()
	conn, err := driver.Open(dsn, func(c *sqlite3.Conn) error {
		if err := c.Exec("PRAGMA busy_timeout = 10000;"); err != nil {
			return err
		}
		return c.Exec("PRAGMA secure_delete = ON;")
	})
	if err != nil {
		return nil, err
	}
	conn.SetMaxOpenConns(1)
	return conn, nil
}

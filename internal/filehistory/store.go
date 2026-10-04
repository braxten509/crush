// Package filehistory retains only versions identified by the tool tracker.
// It never inventories the project and never invokes a Git write operation.
package filehistory

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/charmbracelet/crush/internal/lock"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/crush/internal/filechange"
	"github.com/charmbracelet/crush/internal/secureentry"
)

const ProjectBudget int64 = 2 << 30
const evicted = "Restore data removed to keep this project under 2 GB"

type State struct {
	Exists bool   `json:"exists"`
	Digest string `json:"digest,omitempty"`
	Mode   uint32 `json:"mode,omitempty"`
	Reason string `json:"reason,omitempty"`
	Saved  bool   `json:"saved,omitempty"`
}

type Store struct {
	db                   *sql.DB
	directory            string
	root                 string
	mutex                sync.Mutex
	Budget               int64
	maintenanceMutex     sync.Mutex
	nextMaintenance      time.Time
	maintenanceRunning   bool
	pointMutex           sync.Mutex
	pointAt              time.Time
	pointRoot, pointHead string
}

func New(conn *sql.DB, directory string, root ...string) *Store {
	s := &Store{db: conn, directory: filepath.Join(directory, "file-history", "objects"), Budget: ProjectBudget}
	if len(root) > 0 {
		s.root = root[0]
	}
	return s
}
func hash(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func encode(value any) string { data, _ := json.Marshal(value); return string(data) }
func (s *Store) objectPath(digest string) (string, error) {
	decoded, err := hex.DecodeString(digest)
	if err != nil || len(decoded) != sha256.Size {
		return "", errors.New("invalid saved file digest")
	}
	return filepath.Join(s.directory, digest[:2], digest), nil
}
func (s *Store) put(ctx context.Context, data []byte) (string, error) {
	digest := hash(data)
	path, _ := s.objectPath(digest)
	var size int64
	err := s.db.QueryRowContext(ctx, `SELECT size FROM file_history_objects WHERE digest=?`, digest).Scan(&size)
	if err == nil {
		if _, err = os.Stat(path); err == nil {
			return digest, nil
		}
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return "", err
	}
	var buffer bytes.Buffer
	writer, _ := gzip.NewWriterLevel(&buffer, gzip.BestSpeed)
	if _, err = writer.Write(data); err != nil {
		return "", err
	}
	if err = writer.Close(); err != nil {
		return "", err
	}
	// Register first, so even an interrupted write can be collected without
	// enumerating the object directory.
	_, err = s.db.ExecContext(ctx, `INSERT INTO file_history_objects(digest,size) VALUES(?,?) ON CONFLICT(digest) DO UPDATE SET size=excluded.size`, digest, buffer.Len())
	if err != nil {
		return "", err
	}
	if err = atomicWrite(path, buffer.Bytes(), 0600); err != nil {
		return "", err
	}
	return digest, nil
}
func (s *Store) read(digest string) ([]byte, error) {
	path, err := s.objectPath(digest)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	data, err := io.ReadAll(io.LimitReader(r, filechange.MaxRestoreSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > filechange.MaxRestoreSize || hash(data) != digest {
		return nil, errors.New("saved file failed its integrity check")
	}
	return data, nil
}
func (s *Store) state(ctx context.Context, original *filechange.State) (State, error) {
	if original == nil {
		return State{}, nil
	}
	result := State{Exists: true, Digest: original.Digest, Mode: original.Mode, Reason: original.RestoreOmitted}
	if original.Size > filechange.MaxRestoreSize {
		result.Reason = "Too large to restore (over 10 MB)"
		return result, nil
	}
	if !os.FileMode(original.Mode).IsRegular() {
		result.Reason = "Symbolic links and special files cannot be restored"
		return result, nil
	}
	if result.Reason != "" {
		return result, nil
	}
	if original.RestoreDigestOnly {
		path, err := s.objectPath(original.Digest)
		if err == nil {
			_, err = os.Stat(path)
		}
		if err == nil {
			result.Saved = true
			return result, nil
		}
		result.Reason = "Full contents were not saved for this version"
		return result, nil
	}
	var data []byte
	var err error
	if original.RestoreData != "" {
		data, err = base64.StdEncoding.DecodeString(original.RestoreData)
	} else if original.Omitted == "" {
		data = []byte(original.Content)
	} else {
		result.Reason = "Full contents were not saved for this file"
		return result, nil
	}
	if err != nil {
		return result, err
	}
	if len(data) > filechange.MaxRestoreSize {
		result.Reason = "Too large to restore (over 10 MB)"
		return result, nil
	}
	result.Digest = hash(data)
	_, err = s.put(ctx, data)
	result.Saved = err == nil
	if err != nil {
		result.Reason = "File history unavailable: " + err.Error()
	}
	return result, err
}

// Capture runs before review text is bounded. Repeated message flushes are
// idempotent, while later checkpoints of the same tool have distinct orders.
func (s *Store) Capture(ctx context.Context, sessionID, messageID, toolID string, review *filechange.Review) error {
	if review == nil || review.Summary != nil {
		return nil
	}
	s.mutex.Lock()
	defer s.mutex.Unlock()
	release, lockErr := s.lock(ctx)
	if lockErr == nil {
		defer release()
	} else {
		slog.Warn("File history lock unavailable", "error", lockErr)
	}
	heads := map[string]string{}
	remaining := filechange.MaxRestoreTotal
	for _, change := range review.Changes {
		path := change.Path
		if !filepath.IsAbs(path) {
			path = filepath.Join(review.Root, path)
		}
		path = filepath.Clean(path)
		if filechange.HiddenReviewPath(path, review.Root) || secureentry.Sensitive(path) {
			continue
		}
		var existing int
		if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM file_history_changes WHERE message_id=? AND tool_id=? AND path=? AND capture_order=?`, messageID, toolID, path, change.Order).Scan(&existing); err != nil {
			return err
		}
		if existing != 0 {
			continue
		}
		capture := func(original *filechange.State) State {
			if original == nil {
				return State{}
			}
			copy := *original
			if lockErr != nil {
				copy.RestoreOmitted = "File history unavailable: " + lockErr.Error()
			}
			if copy.Size > int64(remaining) {
				copy.RestoreOmitted = filechange.RestoreBudgetReason
			} else {
				remaining -= int(copy.Size)
			}
			value, err := s.state(ctx, &copy)
			if err != nil {
				slog.Warn("Could not save file version", "path", path, "error", err)
				value.Reason = "File history unavailable: " + err.Error()
			}
			return value
		}
		before := capture(change.Before)
		after := capture(change.After)
		dir := filepath.Dir(path)
		head, ok := heads[dir]
		if !ok {
			head = gitHead(ctx, dir)
			heads[dir] = head
		}
		_, err := s.db.ExecContext(ctx, `INSERT INTO file_history_changes(message_id,session_id,tool_id,path,capture_order,before_state,after_state,git_head) VALUES(?,?,?,?,?,?,?,?)`, messageID, sessionID, toolID, path, change.Order, encode(before), encode(after), head)
		if err != nil {
			return err
		}
	}
	return nil
}
func gitHead(ctx context.Context, directory string) string {
	cmd := exec.CommandContext(ctx, "git", "-C", directory, "rev-parse", "HEAD")
	data, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// Maintain removes unreferenced objects after chat deletion and evicts whole
// oldest chats when the compressed store exceeds its project budget.
func (s *Store) Maintain(ctx context.Context) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	release, err := s.lock(ctx)
	if err != nil {
		return err
	}
	defer release()
	return s.maintain(ctx, "")
}

// The lock lives beside the object folder, so all processes sharing the data
// directory serialize publishing references with collection and eviction.
func (s *Store) lock(ctx context.Context) (func(), error) {
	return lock.File(ctx, filepath.Join(filepath.Dir(filepath.Dir(s.directory)), "file-history.lock"))
}

// ScheduleMaintenance is best effort, throttled and never waits on the caller.
// The application's lifetime context cancels outstanding maintenance on exit.
func (s *Store) ScheduleMaintenance(ctx context.Context) {
	s.maintenanceMutex.Lock()
	if s.maintenanceRunning || time.Now().Before(s.nextMaintenance) {
		s.maintenanceMutex.Unlock()
		return
	}
	s.maintenanceRunning = true
	s.nextMaintenance = time.Now().Add(time.Minute)
	s.maintenanceMutex.Unlock()
	go func() {
		defer func() { s.maintenanceMutex.Lock(); s.maintenanceRunning = false; s.maintenanceMutex.Unlock() }()
		if err := s.Maintain(ctx); err != nil {
			slog.Warn("File history maintenance failed", "error", err)
		}
	}()
}
func (s *Store) maintain(ctx context.Context, keepUndo string) error {
	if err := s.gc(ctx); err != nil {
		return err
	}
	for {
		var size int64
		if err := s.db.QueryRowContext(ctx, `SELECT coalesce(sum(size),0) FROM file_history_objects`).Scan(&size); err != nil {
			return err
		}
		if size <= s.Budget {
			return nil
		}
		var id string
		err := s.db.QueryRowContext(ctx, `SELECT s.id FROM sessions s WHERE EXISTS (SELECT 1 FROM file_history_changes c WHERE c.session_id=s.id AND (json_extract(before_state,'$.saved')=1 OR json_extract(after_state,'$.saved')=1)) OR EXISTS (SELECT 1 FROM file_history_undo u WHERE u.session_id=s.id AND u.id<>?) ORDER BY s.created_at,s.rowid LIMIT 1`, keepUndo).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			return errors.New("file history budget is reserved by undo snapshots")
		}
		if err != nil {
			return err
		}
		_, err = s.db.ExecContext(ctx, `UPDATE file_history_changes SET before_state=CASE WHEN json_extract(before_state,'$.saved')=1 THEN json_set(before_state,'$.saved',json('false'),'$.reason',?) ELSE before_state END, after_state=CASE WHEN json_extract(after_state,'$.saved')=1 THEN json_set(after_state,'$.saved',json('false'),'$.reason',?) ELSE after_state END WHERE session_id=?`, evicted, evicted, id)
		if err != nil {
			return err
		}
		if _, err = s.db.ExecContext(ctx, `DELETE FROM file_history_undo WHERE session_id=? AND id<>?`, id, keepUndo); err != nil {
			return err
		}
		if err = s.gc(ctx); err != nil {
			return err
		}
	}
}
func (s *Store) gc(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT digest FROM file_history_objects WHERE digest NOT IN (
 SELECT json_extract(before_state,'$.digest') FROM file_history_changes WHERE json_extract(before_state,'$.saved')=1
 UNION SELECT json_extract(after_state,'$.digest') FROM file_history_changes WHERE json_extract(after_state,'$.saved')=1
 UNION SELECT json_extract(j.value,'$.before.digest') FROM file_history_undo u,json_each(u.entries) j WHERE json_extract(j.value,'$.before.saved')=1
 UNION SELECT json_extract(j.value,'$.after.digest') FROM file_history_undo u,json_each(u.entries) j WHERE json_extract(j.value,'$.after.saved')=1)`)
	if err != nil {
		return err
	}
	var unused []string
	for rows.Next() {
		var digest string
		if err = rows.Scan(&digest); err != nil {
			rows.Close()
			return err
		}
		unused = append(unused, digest)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, digest := range unused {
		path, err := s.objectPath(digest)
		if err != nil {
			return err
		}
		if err = os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		if _, err = s.db.ExecContext(ctx, `DELETE FROM file_history_objects WHERE digest=?`, digest); err != nil {
			return err
		}
	}
	return nil
}
func atomicWrite(path string, data []byte, mode os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".crush-restore-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(data); err == nil {
		err = f.Chmod(mode.Perm() | mode&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky))
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(name, path); err != nil {
		return err
	}
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func (s State) String() string { return fmt.Sprintf("%t:%s:%d", s.Exists, s.Digest, s.Mode) }

// RecordPoint remembers the project's HEAD at the conversation point itself,
// before later tools can create commits. It does not inspect project files.
func (s *Store) RecordPoint(ctx context.Context, sessionID, messageID string) error {
	if s.root == "" {
		return nil
	}
	s.pointMutex.Lock()
	defer s.pointMutex.Unlock()
	if time.Since(s.pointAt) >= time.Second {
		s.pointRoot, s.pointHead = gitRoot(ctx, s.root), gitHead(ctx, s.root)
		s.pointAt = time.Now()
	}
	root := s.pointRoot
	_, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO file_history_points(message_id,session_id,git_root,git_head) VALUES(?,?,?,?)`, messageID, sessionID, root, s.pointHead)
	return err
}
func gitRoot(ctx context.Context, directory string) string {
	cmd := exec.CommandContext(ctx, "git", "-C", directory, "rev-parse", "--show-toplevel")
	data, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

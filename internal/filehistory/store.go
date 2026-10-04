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
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

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
	db        *sql.DB
	directory string
	root      string
	mutex     sync.Mutex
	Budget    int64
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
	result.Digest, err = s.put(ctx, data)
	result.Saved = err == nil
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
	heads := map[string]string{}
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
		before, err := s.state(ctx, change.Before)
		if err != nil {
			return err
		}
		after, err := s.state(ctx, change.After)
		if err != nil {
			return err
		}
		dir := filepath.Dir(path)
		head, ok := heads[dir]
		if !ok {
			head = gitHead(ctx, dir)
			heads[dir] = head
		}
		_, err = s.db.ExecContext(ctx, `INSERT INTO file_history_changes(message_id,session_id,tool_id,path,capture_order,before_state,after_state,git_head) VALUES(?,?,?,?,?,?,?,?)`, messageID, sessionID, toolID, path, change.Order, encode(before), encode(after), head)
		if err != nil {
			return err
		}
	}
	return s.maintain(ctx, "")
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
	return s.maintain(ctx, "")
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
	root := gitRoot(ctx, s.root)
	_, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO file_history_points(message_id,session_id,git_root,git_head) VALUES(?,?,?,?)`, messageID, sessionID, root, gitHead(ctx, s.root))
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

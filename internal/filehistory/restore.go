package filehistory

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/crush/internal/filechange"
	"github.com/charmbracelet/crush/internal/secureentry"
	"github.com/charmbracelet/crush/internal/ui/diffreview"
	"github.com/google/uuid"
)

type Entry struct {
	Path        string `json:"path"`
	Before      State  `json:"before"`
	After       State  `json:"after"`
	Action      string `json:"action"`
	Adds        int    `json:"adds"`
	Dels        int    `json:"dels"`
	Conflict    string `json:"conflict,omitempty"`
	Unavailable string `json:"unavailable,omitempty"`
	GitMoved    bool   `json:"git_moved,omitempty"`
}
type Plan struct {
	SessionID string
	Target    string
	Source    string
	Revision  int64
	Entries   []Entry
	UndoID    string
}
type Result struct {
	Restored int
	Skipped  []string
	UndoID   string
}

func (r Result) Notice() string {
	text := fmt.Sprintf("Restored %d files.", r.Restored)
	if r.UndoID != "" {
		text += " Use Undo Last File Restore in the command palette to put them back."
	}
	if len(r.Skipped) > 0 {
		text += " Skipped: " + strings.Join(r.Skipped, ", ") + "."
	}
	return text
}

type record struct {
	Path          string
	Before, After State
	Order         int64
	Head          string
}

func (s *Store) path(ctx context.Context, id, target string) ([]string, error) {
	if target == "" {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `WITH RECURSIVE path(id,parent,depth) AS (
 SELECT message_id,parent_id,0 FROM tree_nodes WHERE session_id=? AND message_id=?
 UNION ALL SELECT n.message_id,n.parent_id,p.depth+1 FROM tree_nodes n JOIN path p ON n.message_id=p.parent)
 SELECT id FROM path ORDER BY depth DESC`, id, target)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		result = append(result, id)
	}
	if len(result) == 0 {
		return nil, errors.New("tree entry no longer exists")
	}
	return result, rows.Err()
}
func (s *Store) records(ctx context.Context, ids []string) ([]record, error) {
	var result []record
	for _, id := range ids {
		rows, err := s.db.QueryContext(ctx, `SELECT path,before_state,after_state,capture_order,git_head FROM file_history_changes WHERE message_id=? ORDER BY capture_order,tool_id,path`, id)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var r record
			var before, after string
			if err = rows.Scan(&r.Path, &before, &after, &r.Order, &r.Head); err != nil {
				rows.Close()
				return nil, err
			}
			if err = json.Unmarshal([]byte(before), &r.Before); err != nil {
				rows.Close()
				return nil, err
			}
			if err = json.Unmarshal([]byte(after), &r.After); err != nil {
				rows.Close()
				return nil, err
			}
			result = append(result, r)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	// Actual mutation order wins over completion/message order for parallel tools.
	slices.SortStableFunc(result, func(a, b record) int {
		if a.Order < b.Order {
			return -1
		}
		if a.Order > b.Order {
			return 1
		}
		return 0
	})
	return result, nil
}
func (s *Store) Preview(ctx context.Context, id, target string) (*Plan, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	var current sql.NullString
	plan := &Plan{SessionID: id, Target: target}
	err := s.db.QueryRowContext(ctx, `SELECT message_id,revision FROM tree_heads WHERE session_id=?`, id).Scan(&current, &plan.Revision)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	plan.Source = current.String
	left, err := s.path(ctx, id, current.String)
	if err != nil {
		return nil, err
	}
	right, err := s.path(ctx, id, target)
	if err != nil {
		return nil, err
	}
	common := 0
	for common < len(left) && common < len(right) && left[common] == right[common] {
		common++
	}
	undo, err := s.records(ctx, left[common:])
	if err != nil {
		return nil, err
	}
	redo, err := s.records(ctx, right[common:])
	if err != nil {
		return nil, err
	}
	entries := map[string]*Entry{}
	heads := map[string]string{}
	for i := len(undo) - 1; i >= 0; i-- {
		r := undo[i]
		e := entries[r.Path]
		if e == nil {
			e = &Entry{Path: r.Path, Before: r.After}
			entries[r.Path] = e
		}
		e.After = r.Before
		heads[r.Path] = r.Head
	}
	for _, r := range redo {
		e := entries[r.Path]
		if e == nil {
			e = &Entry{Path: r.Path, Before: r.Before}
			entries[r.Path] = e
		}
		e.After = r.After
		heads[r.Path] = r.Head
	}
	point := target
	if point == "" && len(left) > 0 {
		point = left[0]
	}
	var pointRoot, pointHead string
	pointErr := s.db.QueryRowContext(ctx, `SELECT git_root,git_head FROM file_history_points WHERE message_id=?`, point).Scan(&pointRoot, &pointHead)
	if pointErr != nil && !errors.Is(pointErr, sql.ErrNoRows) {
		return nil, pointErr
	}
	for _, e := range entries {
		if pointRoot != "" && gitRoot(ctx, filepath.Dir(e.Path)) == pointRoot {
			heads[e.Path] = pointHead
		}
		if same(e.Before, e.After) {
			continue
		}
		s.describe(ctx, e, heads[e.Path])
		plan.Entries = append(plan.Entries, *e)
	}
	slices.SortFunc(plan.Entries, func(a, b Entry) int { return strings.Compare(a.Path, b.Path) })
	return plan, nil
}
func same(a, b State) bool {
	return a.Exists == b.Exists && (!a.Exists || a.Digest == b.Digest && a.Mode == b.Mode)
}

// Refuse symlinks in every existing component. Restores never follow a new
// link into another directory, and never replace a directory or special file.
func safePath(path string) error {
	if !filepath.IsAbs(path) || secureentry.Sensitive(path) || filechange.HiddenReviewPath(path, "") {
		return errors.New("path is hidden or protected")
	}
	for part := path; part != filepath.Dir(part); part = filepath.Dir(part) {
		info, err := os.Lstat(part)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("path now contains a symbolic link")
		}
		if part == path && !info.Mode().IsRegular() {
			return errors.New("path is no longer a regular file")
		}
	}
	return nil
}
func currentState(path string) (State, []byte, error) {
	if err := safePath(path); err != nil {
		return State{}, nil, err
	}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return State{}, nil, nil
	}
	if err != nil {
		return State{}, nil, err
	}
	if info.Size() > filechange.MaxRestoreSize {
		return State{}, nil, errors.New("Too large to restore (over 10 MB)")
	}
	f, err := os.Open(path)
	if err != nil {
		return State{}, nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, filechange.MaxRestoreSize+1))
	if err != nil {
		return State{}, nil, err
	}
	if len(data) > filechange.MaxRestoreSize {
		return State{}, nil, errors.New("Too large to restore (over 10 MB)")
	}
	return State{Exists: true, Digest: hash(data), Mode: uint32(info.Mode())}, data, nil
}
func (s *Store) describe(ctx context.Context, e *Entry, head string) {
	e.Action = "restore"
	if !e.After.Exists {
		e.Action = "delete"
	} else if !e.Before.Exists {
		e.Action = "recreate"
	}
	if e.After.Exists && !e.After.Saved {
		e.Unavailable = e.After.Reason
		if e.Unavailable == "" {
			e.Unavailable = "Full contents were not saved"
		}
	}
	if e.Before.Reason != "" {
		e.Unavailable = e.Before.Reason
	}
	state, _, err := currentState(e.Path)
	if err != nil {
		e.Conflict = err.Error()
	} else if !same(state, e.Before) {
		e.Conflict = "Changed outside this branch; will skip"
	}
	e.GitMoved = gitHead(ctx, filepath.Dir(e.Path)) != head
	var before, after []byte
	if e.Before.Exists && e.Before.Saved {
		before, err = s.read(e.Before.Digest)
		if err != nil {
			e.Unavailable = "Saved version is missing or damaged"
		}
	}
	if e.After.Exists && e.After.Saved {
		after, err = s.read(e.After.Digest)
		if err != nil {
			e.Unavailable = "Saved version is missing or damaged"
		}
	}
	if e.Unavailable == "" {
		e.Adds, e.Dels = diffreview.Stats(diffreview.Build([]diffreview.Edit{{Path: e.Path, Before: string(before), After: string(after), Full: true}}))
	}
}
func (s *Store) UndoPreview(ctx context.Context, id string) (*Plan, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	plan := &Plan{SessionID: id}
	var data string
	err := s.db.QueryRowContext(ctx, `SELECT id,entries FROM file_history_undo WHERE session_id=? ORDER BY created_at DESC,rowid DESC LIMIT 1`, id).Scan(&plan.UndoID, &data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("There is no file restore to undo in this chat")
	}
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal([]byte(data), &plan.Entries); err != nil {
		return nil, err
	}
	for i := range plan.Entries {
		e := &plan.Entries[i]
		e.Before, e.After = e.After, e.Before
		s.describe(ctx, e, "")
	}
	return plan, nil
}

// Apply saves ALL before images durably before the first filesystem mutation.
// A stale preview cannot silently approve new files. Each file is checked
// again immediately before its atomic replacement. Partial errors keep undo.
func (s *Store) Apply(ctx context.Context, plan *Plan) (Result, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	var result Result
	if plan.UndoID == "" {
		var revision int64
		var source string
		if err := s.db.QueryRowContext(ctx, `SELECT revision,coalesce(message_id,'') FROM tree_heads WHERE session_id=?`, plan.SessionID).Scan(&revision, &source); err != nil {
			return result, err
		}
		if revision != plan.Revision || source != plan.Source {
			return result, errors.New("The chat moved. Open the file preview again")
		}
	}
	var entries []Entry
	for _, e := range plan.Entries {
		if e.Conflict != "" || e.Unavailable != "" {
			result.Skipped = append(result.Skipped, e.Path)
			continue
		}
		current, data, err := currentState(e.Path)
		if err != nil || !same(current, e.Before) {
			result.Skipped = append(result.Skipped, e.Path)
			continue
		}
		if e.After.Exists {
			if _, err = s.read(e.After.Digest); err != nil {
				return result, err
			}
		}
		if current.Exists {
			current.Digest, err = s.put(ctx, data)
			if err != nil {
				return result, err
			}
			current.Saved = true
		}
		e.Before = current
		entries = append(entries, e)
	}
	if len(entries) == 0 {
		return result, nil
	}
	result.UndoID = uuid.NewString()
	if _, err := s.db.ExecContext(ctx, `INSERT INTO file_history_undo(id,session_id,created_at,entries) VALUES(?,?,?,?)`, result.UndoID, plan.SessionID, time.Now().UnixNano(), encode(entries)); err != nil {
		return Result{}, err
	}
	// Reserve undo space before touching files. Old chat versions may be evicted,
	// but the undo record pins both sides of every file in this restore.
	if err := s.maintain(ctx, result.UndoID); err != nil {
		return result, err
	}
	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		now, _, err := currentState(e.Path)
		if err != nil || !same(now, e.Before) {
			result.Skipped = append(result.Skipped, e.Path)
			continue
		}
		if e.After.Exists {
			var data []byte
			data, err = s.read(e.After.Digest)
			if err == nil {
				err = os.MkdirAll(filepath.Dir(e.Path), 0755)
			}
			if err == nil {
				err = safePath(e.Path)
			}
			if err == nil {
				err = atomicWrite(e.Path, data, os.FileMode(e.After.Mode))
			}
		} else {
			err = os.Remove(e.Path)
			if os.IsNotExist(err) {
				err = nil
			}
		}
		if err != nil {
			return result, fmt.Errorf("could not restore %s (undo snapshot saved): %w", e.Path, err)
		}
		result.Restored++
	}
	return result, nil
}

// A fork's restore belongs to its new chat, where the completion notice appears.
func (s *Store) MoveUndo(ctx context.Context, undoID, sessionID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE file_history_undo SET session_id=? WHERE id=?`, sessionID, undoID)
	return err
}

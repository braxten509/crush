// Package filechange records local filesystem changes independently of Git.
// Only files identified by tools or their actual mutations are inspected;
// directories are never inventoried. Snapshots are for review, never for restoring files.
package filechange

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/crush/internal/secureentry"
)

// Bound previews of the files editing tools identify. Files without a text
// preview still produce a visible change summary.
const (
	maxTextSize  = 1 << 20
	maxTextTotal = 64 << 20
)

// State distinguishes a missing file from an empty file. Nil means missing.
// Omitted explains why Content is unavailable (binary, oversized, unreadable,
// or outside the text budget). Digest identifies content independently of mtime.
type State struct {
	Content string `json:"content,omitempty"`
	Digest  string `json:"digest,omitempty"`
	Size    int64  `json:"size"`
	Mode    uint32 `json:"mode"`
	Omitted string `json:"omitted,omitempty"`
}

type Change struct {
	Path string `json:"path"`
	// Order follows capture time, not the tool call's position in the chat.
	// Parallel tools often finish out of order.
	Order    int64     `json:"order,omitempty"`
	Before   *State    `json:"before,omitempty"`
	After    *State    `json:"after,omitempty"`
	Transfer *Transfer `json:"transfer,omitempty"`
}

// Review is persisted with a tool result, outside the content sent to models.
// A non-nil review also records coverage when no files changed, preventing
// legacy edit metadata from inventing a diff for an unsuccessful/no-op edit.
type Review struct {
	Root      string         `json:"root"`
	Changes   []Change       `json:"changes,omitempty"`
	ID        string         `json:"id,omitempty"`
	SessionID string         `json:"session_id,omitempty"`
	Summary   *ReviewSummary `json:"summary,omitempty"`
}

// ReviewSummary is small enough to keep in a transcript. File contents and
// the complete file list are fetched only when the review is opened.
type ReviewSummary struct {
	Files     int      `json:"files"`
	Paths     []string `json:"paths,omitempty"`
	Copied    int      `json:"copied,omitempty"`
	Checkouts int      `json:"checkouts,omitempty"`
	Moved     int      `json:"moved,omitempty"`
	Generated int      `json:"generated,omitempty"`
}

type entry struct {
	state      State
	info       fs.FileInfo
	changeTime int64
}

type Tracker struct {
	root     string
	exclude  []string
	files    map[string]entry
	extra    map[string]bool
	order    int64
	store    *snapshotStore
	imported bool
}

func New(ctx context.Context, root string, exclude ...string) (*Tracker, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	t := &Tracker{root: root, files: map[string]entry{}, extra: map[string]bool{}}
	for _, path := range exclude {
		if path == "" {
			continue
		}
		if !filepath.IsAbs(path) {
			path = filepath.Join(root, path)
		}
		if resolved, err := filepath.EvalSymlinks(path); err == nil {
			path = resolved
		}
		t.exclude = append(t.exclude, filepath.Clean(path))
	}
	return t, nil
}

func (t *Tracker) Root() string { return t.root }

// Contains says whether an editing tool has explicitly identified this file,
// inside or outside the working folder.
func (t *Tracker) Contains(path string) bool {
	if !filepath.IsAbs(path) {
		path = filepath.Join(t.root, path)
	}
	path = filepath.Clean(path)
	return !t.excluded(path) && t.extra[path]
}

// Track adds an explicitly named file before its tool executes. This is a
// file-level baseline, never a request to discover other files in its folder.
func (t *Tracker) Track(path string) {
	if path == "" {
		return
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(t.root, path)
	}
	path = filepath.Clean(path)
	// An explicitly edited file may be reached through a workspace symlink.
	// Inventory its target as well, without following other directory links.
	if resolved, err := filepath.EvalSymlinks(path); err == nil && resolved != path {
		t.Track(resolved)
	}
	if t.Contains(path) || t.excluded(path) {
		return
	}
	info, err := os.Lstat(path)
	if err == nil && info.IsDir() {
		return
	}
	// Terminals, pipes, sockets and kernel files (/proc, /sys, cgroups) are
	// written all the time by the programs a command starts; they are not
	// edits anyone reviews.
	if err == nil && !info.Mode().IsRegular() && info.Mode()&fs.ModeSymlink == 0 || onKernelFilesystem(path) {
		return
	}
	t.extra[path] = true
	if err != nil {
		return
	}
	budget := maxTextTotal
	for _, e := range t.files {
		budget -= len(e.state.Content)
	}
	t.files[path] = t.readEntry(path, info, &budget)
}

// Checkpoint advances the baseline only after a completed scan. Previously
// returned changes remain immutable, so later edits cannot rewrite old reviews.
func (t *Tracker) Checkpoint(ctx context.Context) (*Review, error) {
	next, err := t.scan(ctx)
	if err != nil {
		return nil, err
	}
	review := &Review{Root: t.root}
	t.order = max(t.order+1, time.Now().UnixNano())
	paths := make(map[string]bool, len(next)+len(t.files))
	for path := range next {
		paths[path] = true
	}
	for path := range t.files {
		paths[path] = true
	}
	for path := range paths {
		if t.excluded(path) {
			delete(t.extra, path)
			continue
		}
		before, had := t.files[path]
		after, has := next[path]
		if had && has && before.state == after.state {
			continue
		}
		change := Change{Path: path, Order: t.order}
		if had {
			state := before.state
			change.Before = &state
		}
		if has {
			state := after.state
			change.After = &state
		}
		review.Changes = append(review.Changes, change)
	}
	slices.SortFunc(review.Changes, func(a, b Change) int { return strings.Compare(a.Path, b.Path) })
	t.files = next
	return review, nil
}

func (t *Tracker) scan(ctx context.Context) (map[string]entry, error) {
	next := make(map[string]entry, len(t.files))
	budget := maxTextTotal
	// Reserve already captured text first so traversal order cannot evict the
	// before image of an unchanged file when a new file consumes the budget.
	for _, previous := range t.files {
		budget -= len(previous.state.Content)
	}
	visit := func(path string, info fs.FileInfo) {
		previous, known := t.files[path]
		if !known && (t.imported || generatedArtifact(path)) {
			reason := "Copied content"
			if !t.imported {
				reason = "Generated build artifact"
			}
			next[path] = entry{info: info, changeTime: changeTime(info), state: State{Size: info.Size(), Mode: uint32(info.Mode()), Omitted: reason}}
			return
		}
		if known && os.SameFile(previous.info, info) && previous.info.Size() == info.Size() &&
			previous.info.Mode() == info.Mode() && previous.info.ModTime().Equal(info.ModTime()) &&
			previous.changeTime == changeTime(info) {
			next[path] = previous
			return
		}
		if known {
			budget += len(previous.state.Content)
		}
		next[path] = t.readEntry(path, info, &budget)
	}
	for path := range t.extra {
		if t.excluded(path) {
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		info, err := os.Lstat(path)
		if err != nil {
			if previous, ok := t.files[path]; ok && !errors.Is(err, fs.ErrNotExist) {
				next[path] = previous
			}
			continue
		}
		if !info.IsDir() {
			visit(path, info)
		}
	}
	return next, nil
}

func (t *Tracker) readEntry(path string, info fs.FileInfo, budget *int) entry {
	if t.store != nil {
		return t.store.read(path, info)
	}
	return readEntry(path, info, budget)
}

func (t *Tracker) excluded(path string) bool {
	if secureentry.Sensitive(path) {
		return true
	}
	// Ignore only VCS bookkeeping and explicit Crush runtime paths. Hidden,
	// untracked and gitignored project files are deliberately included.
	for _, part := range strings.Split(filepath.Clean(path), string(filepath.Separator)) {
		if part == ".git" {
			return true
		}
	}
	for _, root := range t.exclude {
		if within(path, root) {
			return true
		}
	}
	return false
}

func within(path, root string) bool {
	return path == root || strings.HasPrefix(path, strings.TrimRight(root, string(filepath.Separator))+string(filepath.Separator))
}

func readEntry(path string, info fs.FileInfo, budget *int) entry {
	var captured entry
	if !secureentry.ReviewFile(path, func() { captured = readReviewEntry(path, info, budget) }) {
		return entry{info: info, state: State{Omitted: "Secure entry destination"}}
	}
	return captured
}

func readReviewEntry(path string, info fs.FileInfo, budget *int) entry {
	e := entry{info: info, changeTime: changeTime(info), state: State{Size: info.Size(), Mode: uint32(info.Mode())}}
	// A metadata signature lets very large files be reviewed without reading
	// multi-gigabyte build products on every agent turn.
	e.state.Digest = fmt.Sprintf("stat:%d:%d:%d", info.Size(), info.ModTime().UnixNano(), e.changeTime)
	switch {
	case info.Mode()&fs.ModeSymlink != 0:
		target, err := os.Readlink(path)
		if err != nil {
			e.state.Omitted = "Cannot read symbolic link"
			return e
		}
		e.state.Content = target
		e.state.Digest = digest([]byte(target))
		return e
	case !info.Mode().IsRegular():
		e.state.Omitted = "Special file (" + info.Mode().Type().String() + ")"
		return e
	case info.Size() > maxTextSize:
		e.state.Omitted = "File exceeds " + strconv.Itoa(maxTextSize) + " byte text preview limit"
		return e
	}
	f, err := os.Open(path)
	if err != nil {
		e.state.Omitted = "Cannot read file contents"
		return e
	}
	defer f.Close()
	// LimitReader also bounds a file that grew after Lstat.
	data, err := io.ReadAll(io.LimitReader(f, maxTextSize+1))
	if err != nil {
		e.state.Omitted = "Cannot read file contents"
		return e
	}
	e.state.Size = int64(len(data))
	if len(data) > maxTextSize {
		e.state.Omitted = "File grew beyond text preview limit"
		return e
	}
	e.state.Digest = digest(data)
	switch {
	case !utf8.Valid(data) || strings.IndexByte(string(data), 0) >= 0:
		e.state.Omitted = "Binary file"
	case len(data) > *budget:
		e.state.Omitted = "Text preview unavailable (file review memory limit)"
	default:
		e.state.Content = string(data)
		*budget -= len(data)
	}
	return e
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

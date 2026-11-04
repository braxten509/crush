// Package lockedchats keeps locked chats in the Plasma Vault (owner,
// 2026-11-03). A locked chat's rows and files exist only in the vault;
// unlocking copies them back to crush.db and their own places so the chat
// runs as any other, and locking again copies them into the vault and
// wipes the outside copy (PLAN.md in ~/.local/share/crush-locked-chats).
//
// Lock and unlock move through durable phases (sessions.lock_state and
// lock_phase), so a crash at any point is finished or undone on the next
// start without ever leaving zero copies or wiping the only one.
package lockedchats

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/crush/internal/db"
	"github.com/charmbracelet/crush/internal/lock"
	"github.com/charmbracelet/crush/internal/prompthistory"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/charmbracelet/crush/internal/session"
)

// Service locks and unlocks the chats of one data directory.
type Service struct {
	DB *sql.DB
	// DataDir is the folder's .crush; Folder the folder itself.
	DataDir, Folder string
	Sessions        session.Service
	Vault           Vault
	// Gate runs fn while none of the chats has work running and no prompt
	// can start in them; nil runs fn as is (no agent).
	Gate func(ctx context.Context, ids []string, fn func() error) error
	// BeforeLock stops what must stop before a chat leaves (its phone
	// share), under the gate.
	BeforeLock func(ctx context.Context, id string)
	// Shown reports whether an app shows the chat; an unlocked chat no
	// app shows is locked again. Nil turns the watch off.
	Shown func(id string) bool
	// Notify tells the apps an error, e.g. that the vault stayed closed.
	Notify func(sessionID, text string)
	// PromptHistory is the global prompt-history.json.
	PromptHistory string
	// CacheDir is Crush's cache (~/.cache/crush): attachments and uploads.
	CacheDir string
	// OtherAttachmentStores lists other registered folders' database files.
	// Nil is appropriate for an isolated store (including test fixtures).
	OtherAttachmentStores func() ([]string, error)
	// StopWork cancels and drains turns and flushes messages before shutdown locks.
	StopWork func(context.Context, []string) error

	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.Mutex
	// running holds the chats with a lock or unlock under way here.
	running map[string]bool
	wg      sync.WaitGroup
}

// ErrBusyLocking refuses an action while the chat is being locked or
// unlocked.
var ErrBusyLocking = errors.New("the chat is being locked or unlocked; wait a moment")

// crashHook lets tests stop a lock or unlock at a step the way a crash
// would: errCrash skips every clean-up a dead process skips too.
var crashHook = func(step string) error { return nil }

var errCrash = errors.New("crash (test)")

// retryWait spaces retries of a busy checkpoint or gate.
var retryWait = 200 * time.Millisecond

func sleepChan() <-chan time.Time { return time.After(retryWait) }

// Start begins the background work: crash recovery and, with Shown, the
// watch that relocks a chat no app shows. Stop ends it.
func (s *Service) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.ctx != nil {
		s.mu.Unlock()
		return nil
	}
	s.ctx, s.cancel = context.WithCancel(ctx)
	s.running = map[string]bool{}
	s.mu.Unlock()
	if err := s.hideOrphans(ctx); err != nil {
		return err // fail startup closed, before any request is served
	}
	s.goBackground(func(ctx context.Context) { s.Recover(ctx) })
	if s.Shown != nil {
		s.goBackground(s.watch)
	}
	return nil
}

func (s *Service) background() context.Context {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx == nil {
		s.ctx, s.cancel = context.WithCancel(context.Background())
		s.running = map[string]bool{}
	}
	return s.ctx
}

func (s *Service) goBackground(fn func(context.Context)) {
	ctx := s.background()
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		fn(ctx)
	}()
}

// Wait waits for background locks and unlocks (tests, shutdown).
func (s *Service) Wait() { s.wg.Wait() }

// claim marks a chat busy here; false when it already is.
func (s *Service) claim(id string) bool {
	s.background()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running[id] {
		return false
	}
	s.running[id] = true
	return true
}

func (s *Service) release(id string) {
	s.mu.Lock()
	delete(s.running, id)
	s.mu.Unlock()
}

func (s *Service) notify(id string, err error) {
	slog.Warn("Locked chat", "session", id, "error", err)
	if s.Notify != nil {
		s.Notify(id, err.Error())
	}
}

// state reads a chat's lock state and phase.
func (s *Service) state(ctx context.Context, id string) (state, phase string, parent sql.NullString, err error) {
	err = s.DB.QueryRowContext(ctx, `SELECT lock_state, lock_phase, parent_session_id FROM sessions WHERE id = ?`, id).Scan(&state, &phase, &parent)
	return state, phase, parent, err
}

// setState saves and publishes a chat's lock state.
func (s *Service) setState(ctx context.Context, id, state, phase string) error {
	if s.Sessions != nil {
		_, err := s.Sessions.SetLockState(ctx, id, state, phase)
		return err
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE sessions SET lock_state = ?, lock_phase = ? WHERE id = ?`, state, phase, id)
	return err
}

// publish tells the apps a chat's state after a raw transaction.
func (s *Service) publish(ctx context.Context, id string) {
	if publisher, ok := s.Sessions.(pubsub.Publisher[session.Session]); ok {
		if chat, err := s.Sessions.Get(ctx, id); err == nil {
			publisher.Publish(pubsub.UpdatedEvent, chat)
		}
	}
}

// tree lists a chat and its sub-agent chats, all the way down.
func (s *Service) tree(ctx context.Context, id string) ([]string, error) {
	return queryStrings(ctx, s.DB, `WITH RECURSIVE tree(id) AS (
		SELECT ? UNION SELECT s.id FROM sessions s JOIN tree t ON s.parent_session_id = t.id
	) SELECT id FROM tree`, id)
}

// chatLock is the per-chat file lock every lock and unlock holds, across
// Crush processes (PLAN §9.3).
func (s *Service) chatLock(ctx context.Context, id string) (func(), error) {
	dir := filepath.Join(s.DataDir, "locks")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return lock.File(ctx, filepath.Join(dir, safeName(id)+".lock"))
}

// AcquireChat serializes share registration with locking across processes.
func (s *Service) AcquireChat(ctx context.Context, id string) (func(), error) {
	return s.chatLock(ctx, id)
}

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9_-]`)

func safeName(id string) string { return unsafeName.ReplaceAllString(id, "_") }

// gate runs fn with the chats idle.
func (s *Service) gate(ctx context.Context, ids []string, fn func() error) error {
	if s.Gate == nil {
		return fn()
	}
	return s.Gate(ctx, ids, fn)
}

// Lock locks a chat: a normal chat for the first time, or an unlocked one
// again (the lock button, owner 2026-11-03). It refuses while the chat has
// work running, marks it locking at once and finishes in the background.
func (s *Service) Lock(ctx context.Context, id string) error {
	return s.LockWith(ctx, id, "")
}

// LockWith is Lock with the vault password typed in Crush: a closed vault
// is opened with it first, so a wrong password is refused at once. With
// no password a closed vault asks in KDE's box (the terminal UI).
func (s *Service) LockWith(ctx context.Context, id, password string) error {
	state, _, parent, err := s.state(ctx, id)
	if err != nil {
		return err
	}
	if parent.Valid && parent.String != "" {
		return errors.New("only a whole chat can be locked, not a sub-agent's")
	}
	if state != "" && state != session.LockStateUnlocked {
		if state == session.LockStateLocked {
			return nil
		}
		return ErrBusyLocking
	}
	if !s.claim(id) {
		return ErrBusyLocking
	}
	if err := s.openWith(ctx, password); err != nil {
		s.release(id)
		return err
	}
	if err := s.startLock(ctx, id); err != nil {
		s.release(id)
		return err
	}
	s.goBackground(func(ctx context.Context) {
		defer s.release(id)
		if err := s.lockNow(ctx, id); err != nil {
			s.notify(id, err)
		}
	})
	return nil
}

// startLock flips the chat to locking under the gate: from then on no
// prompt runs and nothing writes to it.
func (s *Service) startLock(ctx context.Context, id string) error {
	release, err := s.chatLock(ctx, id)
	if err != nil {
		return err
	}
	defer release()
	ids, err := s.tree(ctx, id)
	if err != nil {
		return err
	}
	return s.gate(ctx, ids, func() error {
		state, _, _, err := s.state(ctx, id)
		if err != nil {
			return err
		}
		if state != "" && state != session.LockStateUnlocked {
			return ErrBusyLocking
		}
		if s.BeforeLock != nil {
			s.BeforeLock(ctx, id)
		}
		return s.setState(ctx, id, session.LockStateLocking, state)
	})
}

// Unlock asks for the vault password and copies a locked chat back. It
// marks the chat unlocking at once and finishes in the background. A chat
// left outside by a crash (relock) is put back into the vault first, then
// unlocked (owner, 2026-11-03: Crush retries on the next Unlock click).
func (s *Service) Unlock(ctx context.Context, id string) error {
	return s.UnlockWith(ctx, id, "")
}

// UnlockWith is Unlock with the vault password typed in Crush.
func (s *Service) UnlockWith(ctx context.Context, id, password string) error {
	state, _, _, err := s.state(ctx, id)
	if err != nil {
		return err
	}
	switch state {
	case session.LockStateLocked, session.LockStateRelock:
	case session.LockStateUnlocked, "":
		return nil
	default:
		return ErrBusyLocking
	}
	if !s.claim(id) {
		return ErrBusyLocking
	}
	if err := s.openWith(ctx, password); err != nil {
		s.release(id)
		return err
	}
	s.goBackground(func(ctx context.Context) {
		defer s.release(id)
		if state == session.LockStateRelock {
			if err := s.lockNow(ctx, id); err != nil {
				s.notify(id, err)
				return
			}
		}
		if err := s.unlockNow(ctx, id); err != nil {
			s.notify(id, err)
		}
	})
	return nil
}

// StopLocking turns a locked chat into a normal one for good, once the
// vault password is given: it is unlocked, its vault copy deleted and its
// lock state cleared (owner, 2026-11-03).
func (s *Service) StopLocking(ctx context.Context, id string) error {
	return s.StopLockingWith(ctx, id, "")
}

// StopLockingWith is StopLocking with the vault password typed in Crush.
func (s *Service) StopLockingWith(ctx context.Context, id, password string) error {
	state, _, _, err := s.state(ctx, id)
	if err != nil {
		return err
	}
	if state == "" {
		return nil
	}
	if state == session.LockStateLocking || state == session.LockStateUnlocking {
		return ErrBusyLocking
	}
	if !s.claim(id) {
		return ErrBusyLocking
	}
	if err := s.openWith(ctx, password); err != nil {
		s.release(id)
		return err
	}
	s.goBackground(func(ctx context.Context) {
		defer s.release(id)
		if err := s.stopNow(ctx, id, state); err != nil {
			s.notify(id, err)
		}
	})
	return nil
}

func (s *Service) stopNow(ctx context.Context, id, state string) error {
	mount, err := s.openVault(ctx)
	if err != nil {
		return err
	}
	if state == session.LockStateLocked {
		if err := s.unlockNow(ctx, id); err != nil {
			return err
		}
	}
	unlock, err := s.chatLock(ctx, id)
	if err != nil {
		return err
	}
	defer unlock()
	if err := os.RemoveAll(filepath.Join(mount, "chats", safeName(id))); err != nil {
		return err
	}
	_ = os.RemoveAll(filepath.Join(mount, "chats", safeName(id)+".next"))
	if err := editIndex(mount, func(index map[string]indexEntry) { delete(index, id) }); err != nil {
		return err
	}
	if err := s.setState(ctx, id, "", ""); err != nil {
		return err
	}
	_ = unregister(s.DataDir, id)
	s.closeVaultIfDone(ctx)
	return nil
}

// RelockAll locks again every chat this Crush unlocked in this folder,
// waiting for busy ones (quitting Crush, owner 2026-11-03). What can't be
// locked now stays for the next start's recovery.
func (s *Service) RelockAll(ctx context.Context) {
	for _, id := range ownedHere(s.DataDir) {
		ids, err := s.tree(ctx, id)
		if err == nil && s.StopWork != nil {
			err = s.StopWork(ctx, ids)
		}
		if err != nil {
			s.notify(id, err)
			continue
		}
		s.relockOwned(ctx, id, -1)
	}
}

// relockOwned locks an unlocked chat again, retrying the gate until wait
// ends.
func (s *Service) relockOwned(ctx context.Context, id string, wait time.Duration) {
	if !s.claim(id) {
		return
	}
	defer s.release(id)
	state, _, _, err := s.state(ctx, id)
	if err != nil || state != session.LockStateUnlocked {
		if errors.Is(err, sql.ErrNoRows) {
			_ = unregister(s.DataDir, id)
		}
		return
	}
	deadline := time.Now().Add(wait)
	for {
		err = s.startLock(ctx, id)
		if err == nil || (wait >= 0 && time.Now().After(deadline)) || ctx.Err() != nil {
			break
		}
		select {
		case <-ctx.Done():
		case <-sleepChan():
		}
	}
	if err != nil {
		slog.Warn("An unlocked chat could not be locked again now", "session", id, "error", err)
		return
	}
	if err := s.lockNow(ctx, id); err != nil {
		s.notify(id, err)
	}
}

// watch relocks an unlocked chat once no app shows it (closed with ×,
// Complete, or every app quit).
func (s *Service) watch(ctx context.Context) {
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		for _, id := range ownedHere(s.DataDir) {
			if !s.Shown(id) {
				s.relockOwned(ctx, id, 0)
			}
		}
	}
}

// Stop ends background work after relocking what this Crush unlocked.
func (s *Service) Stop(ctx context.Context) {
	s.mu.Lock()
	cancel := s.cancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	s.wg.Wait()
	// The watcher and asynchronous unlocks must end before taking the final
	// ownership list; otherwise a claimed chat can be skipped during quit.
	s.RelockAll(ctx)
	s.Recover(ctx)
}

// openVault opens the vault, making it through KDE's wizard first when
// there is none (PLAN §3).
func (s *Service) openVault(ctx context.Context) (string, error) {
	exists, mount, err := s.Vault.Find(ctx)
	if err != nil {
		return "", err
	}
	if mount != "" {
		return mount, nil
	}
	if !exists {
		if err := s.Vault.Create(ctx); err != nil {
			return "", err
		}
		wait, cancel := context.WithTimeout(ctx, openWait)
		defer cancel()
		for !exists {
			select {
			case <-wait.Done():
				return "", ErrNoVault
			case <-time.After(time.Second):
			}
			if exists, mount, err = s.Vault.Find(wait); err != nil {
				return "", err
			}
			if mount != "" {
				return mount, nil
			}
		}
	}
	return s.Vault.Open(ctx)
}

// openWith opens a closed vault with a typed password; nothing to do
// without one or when the vault is already open.
func (s *Service) openWith(ctx context.Context, password string) error {
	if password == "" {
		return nil
	}
	exists, mount, err := s.Vault.Find(ctx)
	if err != nil || mount != "" {
		return err
	}
	if !exists {
		return nil // openVault starts KDE's wizard for a new vault
	}
	_, err = s.Vault.OpenWith(ctx, password)
	return err
}

// VaultState reports whether the vault exists and is open, so an app
// knows whether to ask for the password.
func (s *Service) VaultState(ctx context.Context) (exists, open bool, err error) {
	exists, mount, err := s.Vault.Find(ctx)
	return exists, mount != "", err
}

// closeVaultIfDone closes the vault when no chat is unlocked and no lock
// or unlock runs here.
func (s *Service) closeVaultIfDone(ctx context.Context) {
	s.mu.Lock()
	busy := len(s.running) > 1
	s.mu.Unlock()
	if busy || anyUnlocked() {
		return
	}
	if err := s.Vault.Close(ctx); err != nil {
		slog.Warn("Could not close the locked chats vault", "error", err)
	}
}

// -- the vault copy --

// manifest describes one chat's vault copy (PLAN §2).
type manifest struct {
	Version  int              `json:"version"`
	Root     string           `json:"root"`
	Folder   string           `json:"folder"`
	DataDir  string           `json:"data_dir"`
	Title    string           `json:"title"`
	LockedAt int64            `json:"locked_at"`
	Sessions []string         `json:"sessions"`
	Counts   map[string]int64 `json:"counts"`
	Files    []savedFile      `json:"files"`
	Entries  []savedEntry     `json:"entries"`
	Branches []map[string]any `json:"branches,omitempty"`
}

// savedFile is a file or folder copied into the vault.
type savedFile struct {
	Orig  string            `json:"orig"`
	Vault string            `json:"vault"`
	Dir   bool              `json:"dir,omitempty"`
	Sum   string            `json:"sum,omitempty"`
	Sums  map[string]string `json:"sums,omitempty"`
	Wipe  bool              `json:"wipe"`
}

// savedEntry is a shared store's entries copied into the vault.
type savedEntry struct {
	Ref   entryRef `json:"ref"`
	Vault string   `json:"vault"`
}

type indexEntry struct {
	ID       string `json:"id"`
	Folder   string `json:"folder"`
	DataDir  string `json:"data_dir"`
	Title    string `json:"title"`
	LockedAt int64  `json:"locked_at"`
}

// editIndex changes <mount>/index.json, which lets the locked chats be
// found again even without their crush.db rows (PLAN §6).
func editIndex(mount string, edit func(map[string]indexEntry)) error {
	path := filepath.Join(mount, "index.json")
	index := map[string]indexEntry{}
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &index)
	}
	edit(index)
	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return err
	}
	return writeFileSync(path, data)
}

// pendingWipe is one lock_pending_wipes row.
type pendingWipe struct{ Kind, Target string }

// plan is what one lock moves.
type lockPlan struct {
	files   []savedFile
	entries []entryRef
}

// pathPattern finds absolute paths in a chat's message text.
var pathPattern = regexp.MustCompile(`/[^\s"'\\<>|*?]+`)

// collect lists the chat's files and shared-store entries outside
// crush.db: transcripts, saved file versions, cached files its messages
// name, attachments, its agent CLIs' copies and its entries in Crush's
// shared files. A CLI store in an unknown format fails the lock.
func (s *Service) collect(ctx context.Context, ids []string, digests, exclusive []string) (lockPlan, error) {
	var plan lockPlan
	sharedAttachments, err := s.outsideAttachments(ctx, ids)
	if err != nil {
		return plan, err
	}
	seen := map[string]bool{}
	add := func(path string, dir, wipe bool) {
		if path == "" || seen[path] {
			return
		}
		seen[path] = true
		plan.files = append(plan.files, savedFile{Orig: path, Dir: dir, Wipe: wipe})
	}
	for _, id := range ids {
		name := safeName(id) + ".md"
		for _, base := range []string{s.DataDir, filepath.Join(os.TempDir(), "crush-transcripts")} {
			if path := filepath.Join(base, "transcripts", name); fileExists(path) {
				add(path, false, true)
			}
		}
	}
	isExclusive := map[string]bool{}
	for _, d := range exclusive {
		isExclusive[d] = true
	}
	for _, d := range digests {
		if len(d) < 3 || strings.ContainsAny(d, "/\\.") {
			continue
		}
		path := filepath.Join(s.DataDir, "file-history", "objects", d[:2], d)
		if fileExists(path) {
			add(path, false, isExclusive[d])
		}
	}
	// Cached files the messages name: shell output, uploads, attachments,
	// background job logs.
	prefixes := []string{s.DataDir + string(filepath.Separator)}
	if s.CacheDir != "" {
		prefixes = append(prefixes, s.CacheDir+string(filepath.Separator))
	}
	for _, id := range ids {
		texts, err := queryStrings(ctx, s.DB, `SELECT parts FROM messages WHERE session_id = ?`, id)
		if err != nil {
			return plan, err
		}
		for _, text := range texts {
			// Attachments: the cache keeps them under their sha256. Read
			// from the saved parts, not the message service, which writes.
			for _, data := range binaryParts(text) {
				if s.CacheDir == "" {
					break
				}
				for _, path := range glob(filepath.Join(s.CacheDir, "attachments", sha256Hex(data)+".*")) {
					add(path, false, !sharedAttachments[sha256Hex(data)])
				}
			}
			for _, path := range pathPattern.FindAllString(text, -1) {
				path = strings.TrimRight(path, ".,;:)]}")
				if !hasPrefix(path, prefixes) || seen[path] || !fileExists(path) || strings.HasSuffix(path, "crush.db") {
					continue
				}
				if strings.Contains(path, string(filepath.Separator)+"file-history"+string(filepath.Separator)) {
					continue
				}
				var shared bool
				if err := s.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM messages WHERE session_id NOT IN (`+placeholders(len(ids))+`) AND instr(parts, ?) > 0)`,
					append(anySlice(ids), path)...).Scan(&shared); err != nil {
					return plan, err
				}
				add(path, false, !shared)
			}
		}
	}
	// Agent CLI copies of every linked conversation.
	natives, err := s.natives(ctx, ids)
	if err != nil {
		return plan, err
	}
	for _, n := range natives {
		items, err := storeItems(n)
		if err != nil {
			return plan, err
		}
		for _, item := range items {
			switch {
			case item.File != "":
				add(item.File, false, true)
			case item.Dir != "":
				add(item.Dir, true, true)
			case item.Entry != nil:
				plan.entries = append(plan.entries, *item.Entry)
			}
		}
	}
	plan.entries = append(plan.entries,
		entryRef{Type: "crush-links", Path: filepath.Join(s.DataDir, "cli-sessions.json"), ID: ids[0], IDs: ids},
		entryRef{Type: "crush-tasks", Path: filepath.Join(s.DataDir, "task-history.json"), ID: ids[0], IDs: ids},
	)
	if s.PromptHistory != "" {
		plan.entries = append(plan.entries, entryRef{Type: "crush-prompts", Path: s.PromptHistory, ID: ids[0], IDs: ids})
	}
	return plan, nil
}

// outsideAttachments includes binary references that do not contain a cache
// path at all. Other folders share the same content-addressed cache.
func (s *Service) outsideAttachments(ctx context.Context, ids []string) (map[string]bool, error) {
	shared := map[string]bool{}
	read := func(conn *sql.DB, query string, args ...any) error {
		texts, err := queryStrings(ctx, conn, query, args...)
		if err != nil {
			return err
		}
		for _, text := range texts {
			for _, data := range binaryParts(text) {
				shared[sha256Hex(data)] = true
			}
			for _, path := range pathPattern.FindAllString(text, -1) {
				if filepath.Dir(path) == filepath.Join(s.CacheDir, "attachments") {
					shared[strings.SplitN(filepath.Base(path), ".", 2)[0]] = true
				}
			}
		}
		return nil
	}
	query := `SELECT parts FROM messages`
	if len(ids) > 0 {
		query += ` WHERE session_id NOT IN (` + placeholders(len(ids)) + `)`
	}
	if err := read(s.DB, query, anySlice(ids)...); err != nil {
		return nil, err
	}
	if s.OtherAttachmentStores != nil {
		paths, err := s.OtherAttachmentStores()
		if err != nil {
			return nil, err
		}
		for _, path := range paths {
			if sameData(filepath.Dir(path), s.DataDir) {
				continue
			}
			if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
				continue
			} else if err != nil {
				return nil, err
			}
			conn, err := db.OpenForeign(path)
			if err != nil {
				return nil, err
			}
			err = read(conn, `SELECT parts FROM messages`)
			conn.Close()
			if err != nil {
				return nil, err
			} // uncertain references: never wipe
		}
	}
	return shared, nil
}

// natives lists every agent CLI conversation linked to the chats.
func (s *Service) natives(ctx context.Context, ids []string) ([]Native, error) {
	seen := map[Native]bool{}
	var out []Native
	addNative := func(n Native) {
		if n.ID != "" && n.Kind != "" && !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	var links map[string]struct {
		Native string `json:"native"`
	}
	if data, err := os.ReadFile(filepath.Join(s.DataDir, "cli-sessions.json")); err == nil {
		if err := json.Unmarshal(data, &links); err != nil {
			return nil, fmt.Errorf("cli-sessions.json is damaged, so the chat cannot be locked: %w", err)
		}
	}
	for key, link := range links {
		chat, kind, _ := strings.Cut(key, "/")
		if contains(ids, chat) {
			addNative(Native{Kind: kind, ID: link.Native})
		}
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT DISTINCT cli, native_session FROM cli_message_markers WHERE session_id IN (`+placeholders(len(ids))+`)`, anySlice(ids)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var n Native
		if err := rows.Scan(&n.Kind, &n.ID); err != nil {
			return nil, err
		}
		addNative(n)
	}
	return out, rows.Err()
}

// lockNow copies a chat (locking, relock or unlocked) into the vault and
// takes it out of crush.db and its other places.
func (s *Service) lockNow(ctx context.Context, root string) (err error) {
	release, err := s.chatLock(ctx, root)
	if err != nil {
		return err
	}
	defer release()
	state, prior, _, err := s.state(ctx, root)
	if err != nil {
		return err
	}
	if prior == "wipe" {
		return s.finishLock(ctx, root)
	}
	switch state {
	case session.LockStateLocking, session.LockStateRelock:
	case session.LockStateLocked:
		return nil // another Crush finished it
	default:
		return fmt.Errorf("the chat is %q, not being locked", state)
	}
	// A failure leaves the chat hidden with its outside copy whole: a
	// first lock goes back to normal, any other waits for the next try.
	// Only while the outside rows are still there: once lockTx committed,
	// the chat stays locked whatever fails after.
	defer func() {
		if err == nil || errors.Is(err, errCrash) {
			return
		}
		bg := context.WithoutCancel(ctx)
		now, phase, _, readErr := s.state(bg, root)
		if phase == "wipe" {
			return // committed: recovery must finish wiping/checkpointing
		}
		if readErr != nil || (now != session.LockStateLocking && now != session.LockStateRelock) {
			return
		}
		back := session.LockStateRelock
		if now == session.LockStateLocking && prior == "" {
			back = ""
		}
		_ = s.setState(bg, root, back, "")
		if back == session.LockStateRelock {
			_ = unregister(s.DataDir, root)
		}
	}()
	mount, err := s.openVault(ctx)
	if err != nil {
		return err
	}
	ids, err := s.tree(ctx, root)
	if err != nil {
		return err
	}
	var title string
	var updatedAt int64
	if err := s.DB.QueryRowContext(ctx, `SELECT title, updated_at FROM sessions WHERE id = ?`, root).Scan(&title, &updatedAt); err != nil {
		return err
	}

	conn, err := s.DB.Conn(ctx)
	if err != nil {
		return err
	}
	if err := checkSchema(ctx, conn); err != nil {
		conn.Close()
		return err
	}
	if err := setLockSet(ctx, conn, ids); err != nil {
		conn.Close()
		return err
	}
	digests, err := queryStrings(ctx, conn, `SELECT digest FROM temp.lock_digests`)
	if err != nil {
		conn.Close()
		return err
	}
	exclusive, err := exclusiveDigests(ctx, conn)
	if err != nil {
		conn.Close()
		return err
	}
	branches, err := externalBranches(ctx, conn)
	if err != nil {
		conn.Close()
		return err
	}
	dropLockSet(conn)
	conn.Close()

	plan, err := s.collect(ctx, ids, digests, exclusive)
	if err != nil {
		return err
	}

	// The new copy goes beside the old one and replaces it only when it
	// is complete (PLAN §9.5).
	chats := filepath.Join(mount, "chats")
	final := filepath.Join(chats, safeName(root))
	next := final + ".next"
	if err := os.RemoveAll(next); err != nil {
		return err
	}
	if err := os.MkdirAll(next, 0o700); err != nil {
		return err
	}
	if _, err := db.Connect(ctx, next); err != nil {
		return err
	}
	if err := db.Release(next); err != nil {
		return err
	}
	m := manifest{Version: 1, Root: root, Folder: s.Folder, DataDir: s.DataDir, Title: title, LockedAt: time.Now().Unix(), Sessions: ids, Branches: branches}
	if m.Counts, err = s.copyRows(ctx, ids, filepath.Join(next, "crush.db")); err != nil {
		return err
	}
	if err := crashHook("lock:copying"); err != nil {
		return err
	}
	if err := finishVaultDB(ctx, filepath.Join(next, "crush.db")); err != nil {
		return err
	}
	for i, f := range plan.files {
		f.Vault = filepath.Join("files", fmt.Sprintf("%d-%s", i, filepath.Base(f.Orig)))
		if f.Dir {
			if f.Sums, err = copyDir(f.Orig, filepath.Join(next, f.Vault)); err != nil {
				return err
			}
		} else if f.Sum, err = copyFile(f.Orig, filepath.Join(next, f.Vault)); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return err
		}
		m.Files = append(m.Files, f)
	}
	for i, r := range plan.entries {
		vault := filepath.Join("entries", fmt.Sprintf("%d-%s", i, filepath.Base(r.Path)))
		if err := copyEntry(ctx, r, filepath.Join(next, vault)); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return err
		}
		m.Entries = append(m.Entries, savedEntry{Ref: r, Vault: vault})
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := writeFileSync(filepath.Join(next, "manifest.json"), data); err != nil {
		return err
	}
	if err := writeFileSync(filepath.Join(next, "complete"), []byte("1\n")); err != nil {
		return err
	}
	if err := swapIn(final); err != nil {
		return err
	}
	if err := editIndex(mount, func(index map[string]indexEntry) {
		index[root] = indexEntry{ID: root, Folder: s.Folder, DataDir: s.DataDir, Title: title, LockedAt: m.LockedAt}
	}); err != nil {
		return err
	}

	if err := crashHook("lock:copied"); err != nil {
		return err
	}
	// The vault copy is complete and durable: take the outside copy out.
	var wipes []pendingWipe
	for _, f := range m.Files {
		if !f.Wipe {
			continue
		}
		kind := "file"
		if f.Dir {
			kind = "dir"
		}
		wipes = append(wipes, pendingWipe{kind, f.Orig})
	}
	for _, e := range m.Entries {
		wipes = append(wipes, pendingWipe{"entry", marshalRef(e.Ref)})
	}
	conn, err = s.DB.Conn(ctx)
	if err != nil {
		return err
	}
	if err := setLockSet(ctx, conn, ids); err != nil {
		conn.Close()
		return err
	}
	if _, err = conn.ExecContext(ctx, `ATTACH DATABASE ? AS vault`, filepath.Join(final, "crush.db")); err == nil {
		err = lockTx(ctx, conn, root, updatedAt, exclusive, wipes)
		_, _ = conn.ExecContext(context.Background(), `DETACH DATABASE vault`)
	}
	dropLockSet(conn)
	conn.Close()
	if err != nil {
		return err
	}
	if err := crashHook("lock:committed"); err != nil {
		return err
	}
	return s.finishLock(ctx, root)
}

// A lock is unfinished until both checkpoints and every outside wipe pass.
// Keep the durable wipe phase on any failure so recovery retries it.
func (s *Service) finishLock(ctx context.Context, root string) error {
	fail := func(err error) error {
		s.publish(context.WithoutCancel(ctx), root)
		return fmt.Errorf("locking is not finished; outside copies or the database log still need cleaning; Crush will retry at startup: %w", err)
	}
	// Also covers recovery of locks written by older builds, which marked
	// 'locked' before their wipes and checkpoint completed.
	if _, err := s.DB.ExecContext(ctx, `UPDATE sessions SET lock_state = 'locking', lock_phase = 'wipe' WHERE id = ?`, root); err != nil {
		return fail(err)
	}
	if err := checkpoint(ctx, s.DB); err != nil {
		return fail(err)
	}
	if err := s.runWipes(ctx, root); err != nil {
		return fail(err)
	}
	if err := checkpoint(ctx, s.DB); err != nil {
		return fail(err)
	}
	if _, err := s.DB.ExecContext(ctx, `UPDATE sessions SET lock_state = 'locked', lock_phase = '' WHERE id = ?`, root); err != nil {
		return fail(err)
	}
	if err := checkpoint(ctx, s.DB); err != nil {
		_, resetErr := s.DB.ExecContext(context.WithoutCancel(ctx), `UPDATE sessions SET lock_state = 'locking', lock_phase = 'wipe' WHERE id = ?`, root)
		return fail(errors.Join(err, resetErr))
	}
	_ = unregister(s.DataDir, root)
	s.publish(ctx, root)
	s.closeVaultIfDone(ctx)
	return nil
}

// copyRows copies the chats' crush.db rows into the vault copy at dst.
func (s *Service) copyRows(ctx context.Context, ids []string, dst string) (map[string]int64, error) {
	conn, err := s.DB.Conn(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	if err := setLockSet(ctx, conn, ids); err != nil {
		return nil, err
	}
	defer dropLockSet(conn)
	if _, err := conn.ExecContext(ctx, `ATTACH DATABASE ? AS vault`, dst); err != nil {
		return nil, err
	}
	defer conn.ExecContext(context.Background(), `DETACH DATABASE vault`) //nolint:errcheck
	counts, err := copyRowsOut(ctx, conn)
	if err != nil {
		return nil, err
	}
	// The copy must hold every row the chat has (PLAN §7 check 2).
	have, err := countRows(ctx, conn, "main")
	if err != nil {
		return nil, err
	}
	saved, err := countRows(ctx, conn, "vault")
	if err != nil {
		return nil, err
	}
	for table, n := range have {
		if saved[table] != n {
			return nil, fmt.Errorf("the vault copy has %d rows of %s, the chat %d", saved[table], table, n)
		}
	}
	return counts, nil
}

// finishVaultDB makes the vault copy one durable file: its log written in
// and the database synced (PLAN §9.5).
func finishVaultDB(ctx context.Context, path string) error {
	conn, err := db.OpenForeign(path)
	if err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, `PRAGMA synchronous = FULL`); err != nil {
		conn.Close()
		return err
	}
	if err := checkpoint(ctx, conn); err != nil {
		conn.Close()
		return err
	}
	var mode string
	if err := conn.QueryRowContext(ctx, `PRAGMA journal_mode = DELETE`).Scan(&mode); err != nil {
		conn.Close()
		return err
	}
	if err := conn.Close(); err != nil {
		return err
	}
	return syncPath(path)
}

// swapIn replaces final with final.next: the old copy moves aside, the new
// one in, then the old one goes. recoverSwap undoes a crash in between.
func swapIn(final string) error {
	next, old := final+".next", final+".old"
	_ = os.RemoveAll(old)
	if _, err := os.Stat(final); err == nil {
		if err := os.Rename(final, old); err != nil {
			return err
		}
	}
	if err := os.Rename(next, final); err != nil {
		return err
	}
	if err := syncDir(filepath.Dir(final)); err != nil {
		return err
	}
	return os.RemoveAll(old)
}

// recoverSwap repairs a vault copy a crash left mid-swap: a complete new
// copy wins, else the old one comes back.
func recoverSwap(final string) {
	next, old := final+".next", final+".old"
	if !fileExists(filepath.Join(final, "complete")) {
		switch {
		case fileExists(filepath.Join(next, "complete")):
			_ = os.RemoveAll(final)
			_ = os.Rename(next, final)
		case fileExists(filepath.Join(old, "complete")):
			_ = os.RemoveAll(final)
			_ = os.Rename(old, final)
		}
		_ = syncDir(filepath.Dir(final))
	}
	if fileExists(filepath.Join(final, "complete")) {
		_ = os.RemoveAll(old)
	}
}

// runWipes wipes what lock_pending_wipes lists for the chat, removing each
// row once done, then clears the chat's phase.
func (s *Service) runWipes(ctx context.Context, root string) error {
	rows, err := s.DB.QueryContext(ctx, `SELECT kind, target FROM lock_pending_wipes WHERE session_id = ?`, root)
	if err != nil {
		return err
	}
	var wipes []pendingWipe
	for rows.Next() {
		var w pendingWipe
		if err := rows.Scan(&w.Kind, &w.Target); err != nil {
			rows.Close()
			return err
		}
		wipes = append(wipes, w)
	}
	rows.Close()
	var failed error
	for _, w := range wipes {
		var err error
		switch w.Kind {
		case "file":
			if filepath.Dir(w.Target) == filepath.Join(s.CacheDir, "attachments") {
				// Recheck at the actual wipe, including recovery after a crash:
				// another chat may have acquired a reference after collection.
				var shared map[string]bool
				shared, err = s.outsideAttachments(ctx, nil)
				if err == nil && !shared[strings.SplitN(filepath.Base(w.Target), ".", 2)[0]] {
					err = wipeFile(w.Target)
				}
			} else {
				err = wipeFile(w.Target)
			}
		case "dir":
			err = wipeDir(w.Target)
		case "entry":
			var r entryRef
			if err = json.Unmarshal([]byte(w.Target), &r); err == nil {
				err = removeEntry(ctx, r)
			}
		}
		if err != nil {
			failed = errors.Join(failed, err)
			continue
		}
		if _, err := s.DB.ExecContext(ctx, `DELETE FROM lock_pending_wipes WHERE session_id = ? AND kind = ? AND target = ?`, root, w.Kind, w.Target); err != nil {
			failed = errors.Join(failed, err)
		}
	}
	if failed != nil {
		return failed
	}
	state, _, _, err := s.state(ctx, root)
	if err != nil {
		return err
	}
	if state == session.LockStateLocking || state == session.LockStateLocked {
		return nil // finishLock clears the phase after checkpointing
	}
	return s.setState(ctx, root, state, "")
}

// unlockNow copies a locked chat back from the vault.
func (s *Service) unlockNow(ctx context.Context, root string) (err error) {
	release, err := s.chatLock(ctx, root)
	if err != nil {
		return err
	}
	defer release()
	state, _, _, err := s.state(ctx, root)
	if err != nil {
		return err
	}
	if state == session.LockStateUnlocked {
		return nil
	}
	if state != session.LockStateLocked && state != session.LockStateUnlocking {
		return fmt.Errorf("the chat is %q, not locked", state)
	}
	if err := s.setState(ctx, root, session.LockStateUnlocking, "restore"); err != nil {
		return err
	}
	// A failure takes back what was copied out: the vault copy stays the
	// only one, as before.
	defer func() {
		if err == nil || errors.Is(err, errCrash) {
			return
		}
		bg := context.WithoutCancel(ctx)
		_ = unregister(s.DataDir, root)
		if wipeErr := s.runWipes(bg, root); wipeErr != nil {
			slog.Warn("A failed unlock left copies to wipe at the next start", "session", root, "error", wipeErr)
		}
		_ = s.setState(bg, root, session.LockStateLocked, "")
	}()
	mount, err := s.openVault(ctx)
	if err != nil {
		return err
	}
	dir := filepath.Join(mount, "chats", safeName(root))
	recoverSwap(dir)
	if !fileExists(filepath.Join(dir, "complete")) {
		return errors.New("the vault has no complete copy of this chat")
	}
	data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return err
	}
	var m manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}
	// Every place a copy goes back to is listed before it is written, so a
	// crash can take them out again (PLAN §2 unlock step 2).
	for _, f := range m.Files {
		kind := "file"
		if f.Dir {
			kind = "dir"
		}
		if fileExists(f.Orig) {
			continue // a shared saved-file version still here
		}
		if _, err := s.DB.ExecContext(ctx, `INSERT OR IGNORE INTO lock_pending_wipes(session_id, kind, target) VALUES (?, ?, ?)`, root, kind, f.Orig); err != nil {
			return err
		}
	}
	for _, e := range m.Entries {
		if _, err := s.DB.ExecContext(ctx, `INSERT OR IGNORE INTO lock_pending_wipes(session_id, kind, target) VALUES (?, 'entry', ?)`, root, marshalRef(e.Ref)); err != nil {
			return err
		}
	}
	for _, f := range m.Files {
		if fileExists(f.Orig) {
			continue
		}
		src := filepath.Join(dir, f.Vault)
		if f.Dir {
			sums, err := copyDir(src, f.Orig)
			if err != nil {
				return err
			}
			for rel, sum := range f.Sums {
				if sums[rel] != sum {
					return fmt.Errorf("the vault copy of %s is damaged", filepath.Join(f.Orig, rel))
				}
			}
			continue
		}
		sum, err := copyFile(src, f.Orig)
		if err != nil {
			return err
		}
		if sum != f.Sum {
			return fmt.Errorf("the vault copy of %s is damaged", f.Orig)
		}
	}
	for _, e := range m.Entries {
		if err := restoreEntry(ctx, e.Ref, filepath.Join(dir, e.Vault)); err != nil {
			// The CLI starts over from Crush's own copy (ErrResume).
			slog.Warn("An agent CLI's copy of a locked chat did not come back", "path", e.Ref.Path, "error", err)
		}
	}
	if err := crashHook("unlock:restored"); err != nil {
		return err
	}
	vaultDir := dir
	if _, err := db.Connect(ctx, vaultDir); err != nil {
		return err
	}
	if err := db.Release(vaultDir); err != nil {
		return err
	}
	// Registered before the chat shows as unlocked, so no other Crush's
	// recovery takes it for a crash.
	if err := register(s.DataDir, root); err != nil {
		return err
	}
	if err := s.restoreRows(ctx, root, filepath.Join(vaultDir, "crush.db"), m); err != nil {
		return err
	}
	// Only once the connection is back: crush.db has one.
	s.publish(ctx, root)
	return nil
}

// restoreRows puts the vault copy's rows back on a connection of its own,
// released before it returns.
func (s *Service) restoreRows(ctx context.Context, root, vaultDB string, m manifest) error {
	conn, err := s.DB.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := setLockSet(ctx, conn, m.Sessions); err != nil {
		return err
	}
	defer dropLockSet(conn)
	if _, err := conn.ExecContext(ctx, `ATTACH DATABASE ? AS vault`, vaultDB); err != nil {
		return err
	}
	defer conn.ExecContext(context.Background(), `DETACH DATABASE vault`) //nolint:errcheck
	want, err := countRows(ctx, conn, "vault")
	if err != nil {
		return err
	}
	return unlockTx(ctx, conn, root, m.Branches, want)
}

func fileExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func hasPrefix(path string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

func placeholders(n int) string {
	if n <= 0 {
		return "NULL"
	}
	return "?" + strings.Repeat(",?", n-1)
}

func anySlice(ids []string) []any {
	out := make([]any, len(ids))
	for i, id := range ids {
		out[i] = id
	}
	return out
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// PromptHistoryPath is the global prompt history, for the service's
// PromptHistory.
func PromptHistoryPath() string { return prompthistory.Path() }

// binaryParts returns the data of a message's binary parts, as saved in
// messages.parts.
func binaryParts(parts string) [][]byte {
	var wrapped []struct {
		Type string          `json:"type"`
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal([]byte(parts), &wrapped) != nil {
		return nil
	}
	var out [][]byte
	for _, w := range wrapped {
		if w.Type != "binary" {
			continue
		}
		var bin struct{ Data []byte }
		if json.Unmarshal(w.Data, &bin) == nil && len(bin.Data) > 0 {
			out = append(out, bin.Data)
		}
	}
	return out
}

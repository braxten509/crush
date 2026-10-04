package filehistory

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/crush/internal/db"
	"github.com/charmbracelet/crush/internal/filechange"
	"github.com/stretchr/testify/require"
)

func TestUnavailableObjectStorageKeepsPreviewReason(t *testing.T) {
	f := setup(t)
	require.NoError(t, os.WriteFile(filepath.Dir(f.store.directory), []byte("blocked"), 0600))
	p := filepath.Join(f.root, "file")
	f.change(t, "a", p, state("before"), state("after"))
	f.write(t, p, "after")
	preview, err := f.store.Preview(t.Context(), "chat", "")
	require.NoError(t, err)
	require.Len(t, preview.Entries, 1)
	require.Contains(t, preview.Entries[0].Unavailable, "File history unavailable")
	result, err := f.store.Apply(t.Context(), preview)
	require.NoError(t, err)
	require.Zero(t, result.Restored)
	require.Equal(t, "after", contents(t, p))
}

func fakeGit(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	calls := filepath.Join(dir, "calls")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "git"), []byte("#!/bin/sh\nprintf 'call\\n' >> \"$RESTORE_GIT_CALLS\"\ncase \"$*\" in *--show-toplevel*) echo /fixture;; *) echo \"${RESTORE_HEAD:-initial}\";; esac\n"), 0755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("RESTORE_GIT_CALLS", calls)
	return calls
}

func TestPointAndPreviewCacheGitProcesses(t *testing.T) {
	f := setup(t)
	calls := fakeGit(t)
	f.store.root = f.root
	for i := 0; i < 30; i++ {
		require.NoError(t, f.store.RecordPoint(t.Context(), "chat", fmt.Sprint(i)))
	}
	require.Equal(t, 2, strings.Count(contents(t, calls), "call\n"))
	for i := 0; i < 10; i++ {
		p := filepath.Join(f.root, fmt.Sprint(i))
		f.change(t, fmt.Sprint(i), p, state("before"), state("after"))
		f.write(t, p, "after")
	}
	require.NoError(t, os.WriteFile(calls, nil, 0600))
	_, err := f.store.Preview(t.Context(), "chat", "")
	require.NoError(t, err)
	require.Equal(t, 2, strings.Count(contents(t, calls), "call\n"))
}

func TestUndoWarnsOnlyWhenHeadReallyMoved(t *testing.T) {
	f := setup(t)
	fakeGit(t)
	p := filepath.Join(f.root, "file")
	f.change(t, "a", p, state("before"), state("after"))
	f.write(t, p, "after")
	plan, err := f.store.Preview(t.Context(), "chat", "")
	require.NoError(t, err)
	_, err = f.store.Apply(t.Context(), plan)
	require.NoError(t, err)
	undo, err := f.store.UndoPreview(t.Context(), "chat")
	require.NoError(t, err)
	require.False(t, undo.Entries[0].GitMoved)
	t.Setenv("RESTORE_HEAD", "changed")
	undo, err = f.store.UndoPreview(t.Context(), "chat")
	require.NoError(t, err)
	require.True(t, undo.Entries[0].GitMoved)
}

func TestParentsRejectSymlinkBeforeCreatingDirectories(t *testing.T) {
	f := setup(t)
	outside := filepath.Join(f.root, "outside")
	require.NoError(t, os.Mkdir(outside, 0755))
	require.NoError(t, os.Symlink(outside, filepath.Join(f.root, "link")))
	path := filepath.Join(f.root, "link", "must-not-exist", "file")
	require.Error(t, safeParents(path))
	require.NoDirExists(t, filepath.Join(outside, "must-not-exist"))
	good := filepath.Join(f.root, "new", "nested", "file")
	require.NoError(t, safeParents(good))
	require.DirExists(t, filepath.Dir(good))
}

func TestCaptureDoesNotCollectOnEveryResult(t *testing.T) {
	f := setup(t)
	digest, err := f.store.put(t.Context(), []byte("orphan"))
	require.NoError(t, err)
	f.change(t, "a", filepath.Join(f.root, "file"), state("before"), state("after"))
	_, err = f.store.read(digest)
	require.NoError(t, err, "capture must not run the full unreferenced-object scan")
	require.NoError(t, f.store.Maintain(t.Context()))
	_, err = f.store.read(digest)
	require.Error(t, err)
	f.store.nextMaintenance = time.Now().Add(time.Hour)
	f.store.ScheduleMaintenance(t.Context()) // throttled, even if called repeatedly
	require.True(t, time.Until(f.store.nextMaintenance) > 59*time.Minute)
}

func TestGCWorker(t *testing.T) {
	directory := os.Getenv("RESTORE_GC_DATABASE")
	if directory == "" {
		return
	}
	conn, err := db.Connect(t.Context(), directory)
	require.NoError(t, err)
	defer db.Release(directory)
	ctx, cancel := context.WithTimeout(t.Context(), 250*time.Millisecond)
	defer cancel()
	err = New(conn, directory).Maintain(ctx)
	require.ErrorContains(t, err, "deadline exceeded")
}

func TestAnotherProcessCannotCollectUnpublishedCaptureOrUndo(t *testing.T) {
	for _, kind := range []string{"capture", "undo"} {
		t.Run(kind, func(t *testing.T) {
			f := setup(t)
			release, err := f.store.lock(t.Context())
			require.NoError(t, err)
			defer func() {
				if release != nil {
					release()
				}
			}()
			digest, err := f.store.put(t.Context(), []byte("new object"))
			require.NoError(t, err)
			directory := filepath.Dir(filepath.Dir(f.store.directory))
			child := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestGCWorker$")
			child.Env = append(os.Environ(), "RESTORE_GC_DATABASE="+directory)
			output, err := child.CombinedOutput()
			require.NoError(t, err, string(output))
			saved := State{Exists: true, Digest: digest, Saved: true, Mode: 0644}
			if kind == "capture" {
				_, err = f.conn.Exec(`INSERT INTO file_history_changes(message_id,session_id,tool_id,path,capture_order,before_state,after_state,git_head) VALUES('pending','chat','tool','file',1,?,?,'')`, encode(saved), encode(State{}))
			} else {
				_, err = f.conn.Exec(`INSERT INTO file_history_undo(id,session_id,created_at,entries) VALUES('undo','chat',1,?)`, encode([]Entry{{Before: saved}}))
			}
			require.NoError(t, err)
			release()
			release = nil
			require.NoError(t, New(f.conn, directory).Maintain(t.Context()))
			data, err := f.store.read(digest)
			require.NoError(t, err)
			require.Equal(t, "new object", string(data))
		})
	}
}

func TestDigestOnlySnapshotReusesPublishedVersion(t *testing.T) {
	f := setup(t)
	p := filepath.Join(f.root, "large")
	f.change(t, "first", p, state("before"), state("saved"))
	f.change(t, "second", p, &filechange.State{Digest: hash([]byte("saved")), Mode: 0644, Size: 5, RestoreDigestOnly: true}, state("later"))
	f.write(t, p, "later")
	plan, err := f.store.Preview(t.Context(), "chat", "first")
	require.NoError(t, err)
	require.Empty(t, plan.Entries[0].Unavailable)
	_, err = f.store.Apply(t.Context(), plan)
	require.NoError(t, err)
	require.Equal(t, "saved", contents(t, p))
}

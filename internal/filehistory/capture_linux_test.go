//go:build linux && amd64

package filehistory

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/charmbracelet/crush/internal/db"
	"github.com/charmbracelet/crush/internal/filechange"
	"github.com/stretchr/testify/require"
)

func TestTrackerFullBeforeAfterBeyondReviewLimit(t *testing.T) {
	f := setup(t)
	path := filepath.Join(f.root, "large.bin")
	before := bytes.Repeat([]byte{255, 0, 42}, 700000)
	after := bytes.Repeat([]byte{0, 3, 254}, 900000)
	require.NoError(t, os.WriteFile(path, before, 0640))
	tracker, err := filechange.New(t.Context(), f.root)
	require.NoError(t, err)
	tracker.Track(path)
	require.NoError(t, os.WriteFile(path, after, 0640))
	review, err := tracker.Checkpoint(t.Context())
	require.NoError(t, err)
	require.Len(t, review.Changes, 1)
	require.NotEmpty(t, review.Changes[0].Before.Omitted)
	require.NotEmpty(t, review.Changes[0].After.Omitted)
	_, err = f.q.CreateMessage(t.Context(), db.CreateMessageParams{ID: "tool", SessionID: "chat", Role: "tool", Parts: "[]"})
	require.NoError(t, err)
	require.NoError(t, f.store.Capture(t.Context(), "chat", "tool", "call", review))
	plan, err := f.store.Preview(t.Context(), "chat", "")
	require.NoError(t, err)
	require.Empty(t, plan.Entries[0].Conflict)
	require.Empty(t, plan.Entries[0].Unavailable)
	_, err = f.store.Apply(t.Context(), plan)
	require.NoError(t, err)
	require.Equal(t, string(before), contents(t, path))
}
func TestShellCreatedFileCapturedAndRestored(t *testing.T) {
	f := setup(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", `read -r ready; /bin/sh -c "$1"`, "fixture", `printf 'from shell\n' > created.txt`)
	cmd.Dir = f.root
	cmd.Stdout = io.Discard
	cmd.Stderr = os.Stderr
	input, err := cmd.StdinPipe()
	require.NoError(t, err)
	observer, err := filechange.StartProcess(cmd, f.root)
	require.NoError(t, err)
	observer.Begin("call", `printf 'from shell\n' > created.txt`)
	_, err = io.WriteString(input, "go\n")
	require.NoError(t, err)
	input.Close()
	require.NoError(t, observer.Wait(cmd))
	review := observer.End("call")
	require.NotNil(t, review)
	require.Len(t, review.Changes, 1)
	require.Nil(t, review.Changes[0].Before)
	_, err = f.q.CreateMessage(t.Context(), db.CreateMessageParams{ID: "tool", SessionID: "chat", Role: "tool", Parts: "[]"})
	require.NoError(t, err)
	require.NoError(t, f.store.Capture(t.Context(), "chat", "tool", "call", review))
	plan, err := f.store.Preview(t.Context(), "chat", "")
	require.NoError(t, err)
	require.Equal(t, "delete", plan.Entries[0].Action)
	_, err = f.store.Apply(t.Context(), plan)
	require.NoError(t, err)
	require.NoFileExists(t, filepath.Join(f.root, "created.txt"))
	undo, err := f.store.UndoPreview(t.Context(), "chat")
	require.NoError(t, err)
	_, err = f.store.Apply(t.Context(), undo)
	require.NoError(t, err)
	require.Equal(t, "from shell\n", contents(t, filepath.Join(f.root, "created.txt")))
}

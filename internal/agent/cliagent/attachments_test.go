package cliagent

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/charmbracelet/crush/internal/message"
	"github.com/stretchr/testify/require"
)

// The attachment folder drops files unused for too long and the least
// recently used beyond the cap.
func TestPruneAttachments(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	write := func(name string, used time.Time) {
		path := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(path, []byte("png"), 0o600))
		require.NoError(t, os.Chtimes(path, used, used))
	}
	write("stale.png", now.Add(-attachmentMaxAge-time.Hour))
	for i := range attachmentKeep + 1 {
		write(fmt.Sprintf("%03d.png", i), now.Add(-time.Duration(i)*time.Minute))
	}

	pruneAttachments(dir, now)

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, attachmentKeep)
	require.NoFileExists(t, filepath.Join(dir, "stale.png"))
	require.NoFileExists(t, filepath.Join(dir, fmt.Sprintf("%03d.png", attachmentKeep)))
	require.FileExists(t, filepath.Join(dir, "000.png"))
}

// Resending an image marks its copy as used, so it outlives older ones.
func TestSaveImageTouchesResentCopy(t *testing.T) {
	image := message.Attachment{MimeType: "image/png", Content: []byte("resent")}
	path, err := saveImage(image)
	require.NoError(t, err)
	old := time.Now().Add(-attachmentMaxAge + time.Hour)
	require.NoError(t, os.Chtimes(path, old, old))

	again, err := saveImage(image)
	require.NoError(t, err)
	require.Equal(t, path, again)
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.WithinDuration(t, time.Now(), info.ModTime(), time.Minute)
}

package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The oldest folders go first, the current chat's folder never does.
func TestTrimChatFolders(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	start := time.Now().Add(-time.Hour)
	for i := range 6 {
		dir := filepath.Join(root, fmt.Sprintf("chat%d", i))
		require.NoError(t, os.Mkdir(dir, 0o700))
		used := start.Add(time.Duration(i) * time.Minute)
		require.NoError(t, os.Chtimes(dir, used, used))
	}
	// chat0 is the oldest but is the current chat.
	trimChatFolders(root, "chat0", 4)
	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	require.ElementsMatch(t, []string{"chat0", "chat3", "chat4", "chat5"}, names)
}

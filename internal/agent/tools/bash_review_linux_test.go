//go:build linux && amd64

package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/crush/internal/filechange"
	"github.com/stretchr/testify/require"
)

func TestNativeBashReviewsScriptsAndRedirections(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "existing"), []byte("before\n"), 0600))
	command := `printf 'first\n' > created
python3 - <<'PY'
from pathlib import Path
Path('existing').write_text('after\n')
Path('created').write_text('second\n')
PY
printf 'last\n' >> created`
	response := runBashTool(t, newBashToolForTest(root), context.WithValue(t.Context(), SessionIDContextKey, "native-review"), BashParams{Command: command})
	require.False(t, response.IsError, response.Content)
	_, review := filechange.TakeReview(response.Metadata)
	require.NotNil(t, review)
	require.Len(t, review.Changes, 2)
	require.Equal(t, filepath.Join(root, "created"), review.Changes[0].Path)
	require.Nil(t, review.Changes[0].Before)
	require.Equal(t, "second\nlast\n", review.Changes[0].After.Content)
	require.Equal(t, "before\n", review.Changes[1].Before.Content)
	require.Equal(t, "after\n", review.Changes[1].After.Content)
}

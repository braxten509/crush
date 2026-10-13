package chat

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

// One file is named; several of one kind are a count; a mix is "changed".
func TestFileChangeLabel(t *testing.T) {
	t.Parallel()
	require.Equal(t, "edited main.go", fileChangeLabel(map[string]int{"edited": 1}, "main.go"))
	require.Equal(t, "moved notes.md", fileChangeLabel(map[string]int{"moved": 1}, "notes.md"))
	require.Equal(t, "removed 1 file", fileChangeLabel(map[string]int{"removed": 1}, ""), "no name known")
	require.Equal(t, "moved 3 files", fileChangeLabel(map[string]int{"moved": 3}, ""))
	require.Equal(t, "edited 4 files", fileChangeLabel(map[string]int{"edited": 4, "copied": 0}, ""))
	require.Equal(t, "6 files changed", fileChangeLabel(map[string]int{"edited": 3, "moved": 2, "removed": 1}, ""))
	require.Empty(t, fileChangeLabel(map[string]int{}, ""))

	long := fileChangeLabel(map[string]int{"edited": 1}, "a_really_long_generated_file_name_handler.rs")
	name := strings.TrimPrefix(long, "edited ")
	require.LessOrEqual(t, ansi.StringWidth(name), maxFileNameWidth, long)
	require.True(t, strings.HasSuffix(name, ".rs"), "the end of the name stays: %s", long)
	require.Contains(t, name, "…")
}

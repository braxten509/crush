package chat

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

// One file is named; several files are counted; a long name keeps its end.
func TestFileChangeLabel(t *testing.T) {
	t.Parallel()
	require.Contains(t, fileChangeLabel(map[string]int{"edited": 1}, "main.go"), "main.go")
	require.Contains(t, fileChangeLabel(map[string]int{"moved": 1}, "notes.md"), "notes.md")
	require.Contains(t, fileChangeLabel(map[string]int{"removed": 1}, ""), "1", "no name known")
	require.Contains(t, fileChangeLabel(map[string]int{"moved": 3}, ""), "3")
	require.Contains(t, fileChangeLabel(map[string]int{"edited": 4, "copied": 0}, ""), "4")
	require.Contains(t, fileChangeLabel(map[string]int{"edited": 3, "moved": 2, "removed": 1}, ""), "6")
	require.Empty(t, fileChangeLabel(map[string]int{}, ""))

	long := fileChangeLabel(map[string]int{"edited": 1}, "a_really_long_generated_file_name_handler.rs")
	name := long[strings.LastIndex(long, " ")+1:]
	require.LessOrEqual(t, ansi.StringWidth(name), maxFileNameWidth, long)
	require.True(t, strings.HasSuffix(name, ".rs"), "the end of the name stays: %s", long)
	require.Contains(t, name, "…")
}

package list

import (
	"testing"

	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/stretchr/testify/require"
)

func TestSourceSelectionWhitespaceEdges(t *testing.T) {
	for _, tt := range []struct {
		name, source, rendered, want         string
		startLine, startCol, endLine, endCol int
	}{
		{"trailing word space", "foo bar", "  foo bar", "foo ", 0, 2, 0, 6},
		{"indentation only", "    foo", "      foo", "  ", 0, 2, 0, 4},
		{"tab indentation", "\tfoo", "      foo", "\t", 0, 2, 0, 6},
		{"trailing spaces at EOF", "foo  ", "  foo  ", "foo  ", 0, 2, 0, 7},
		{"blank lines", "\nfoo\n\n", "\n\n  foo\n\n", "\nfoo\n\n", 1, 0, 4, 0},
		{"gutter only", "foo", "  foo", "", 0, 0, 0, 2},
		{"blank line gutter", "foo\n    \nbar", "  foo\n      \n  bar", "", 1, 0, 1, 2},
		{"blank line indentation", "foo\n    \nbar", "  foo\n      \n  bar", "    ", 1, 2, 1, 6},
		{"whitespace-only block", "    \n", "      \n", "    \n", 0, 0, 1, 40},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := HighlightSource([]string{tt.source}, tt.rendered, uv.Rect(0, 0, 40, lipgloss.Height(tt.rendered)),
				tt.startLine, tt.startCol, tt.endLine, tt.endCol)
			require.True(t, ok)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestSourceBlankLineAfterWrappedLine(t *testing.T) {
	for _, tt := range []struct {
		start, end int
		want       string
	}{
		{0, 2, ""}, {2, 6, "    "},
	} {
		got, ok := HighlightSource([]string{"abcdefghij\n    \n"}, "  abcdef\n  ghij\n      \n  ",
			uv.Rect(0, 0, 8, 4), 2, tt.start, 2, tt.end)
		require.True(t, ok)
		require.Equal(t, tt.want, got)
	}
}

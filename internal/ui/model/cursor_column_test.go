package model

import (
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/stretchr/testify/require"
)

// The composer's cursor sits on its first text column, after the prompt.
func TestComposerCursorStartsOnFirstTextColumn(t *testing.T) {
	u := newLoadTestUI(t, newLoadWorkspace())
	u.width, u.height = 100, 30
	u.header = newHeader(u.com)
	u.focus = uiFocusEditor
	u.textarea.Focus()
	u.updateLayoutAndSize()
	scr := uv.NewScreenBuffer(u.width, u.height)
	cur := u.Draw(scr, scr.Bounds())
	require.NotNil(t, cur)
	require.Equal(t, u.layout.editor.Min.X+promptWidth, cur.X)
	require.Equal(t, u.layout.editor.Min.Y+editorTextTop, cur.Y)
}

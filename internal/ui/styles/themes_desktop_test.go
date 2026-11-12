package styles

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestDesktopBuiltinsResolve: the desktop's two built-ins are listed and
// load, and the edition's override applies only while no user file shadows
// it (G-2, design 3.7).
func TestDesktopBuiltinsResolve(t *testing.T) {
	setTestThemeDirs(t, []string{t.TempDir()})

	require.Contains(t, BuiltinThemeNames(), "scholars-edition")
	require.Contains(t, BuiltinThemeNames(), "charcoal-amber")

	edition, err := ThemePalette("scholars-edition")
	require.NoError(t, err)
	require.NotEmpty(t, edition.DiffInsertFg)
	s, err := LoadTheme("scholars-edition")
	require.NoError(t, err)
	require.NotNil(t, s.Markdown.CodeBlock.Chroma.Keyword.Color)
	keyword := *s.Markdown.CodeBlock.Chroma.Keyword.Color

	amber, err := ThemePalette("charcoal-amber")
	require.NoError(t, err)
	require.NotEmpty(t, amber.DiffInsertCodeBg, "charcoal-amber's diff roles are derived")
	_, err = LoadTheme("charcoal-amber")
	require.NoError(t, err)

	dir := ThemeDirs()[0]
	require.NoError(t, os.WriteFile(filepath.Join(dir, "scholars-edition.json"), []byte(`{"base":"scholars-edition","keyword":"#123456"}`), 0o644))
	shadowed, err := LoadTheme("scholars-edition")
	require.NoError(t, err)
	require.NotEqual(t, keyword, *shadowed.Markdown.CodeBlock.Chroma.Keyword.Color, "a shadowing file loses the override")
}

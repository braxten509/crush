package styles

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestDesktopBuiltinsResolve: the desktop's two built-ins are listed,
// load, carry the design's colors, and the edition's override applies
// only while no user file shadows it (G-2, design 3.7).
func TestDesktopBuiltinsResolve(t *testing.T) {
	setTestThemeDirs(t, []string{t.TempDir()})

	require.Contains(t, BuiltinThemeNames(), "scholars-edition")
	require.Contains(t, BuiltinThemeNames(), "charcoal-amber")

	edition, err := ThemePalette("scholars-edition")
	require.NoError(t, err)
	require.Equal(t, "#f9f8ef", edition.BgBase)
	require.Equal(t, "#e52b0e", edition.Primary)
	require.Equal(t, "#2f5a1e", edition.DiffInsertFg)
	s, err := LoadTheme("scholars-edition")
	require.NoError(t, err)
	require.Equal(t, "#6a3d12", *s.Markdown.Code.Color)
	require.Equal(t, "#0f0f0f", *s.Markdown.CodeBlock.Chroma.Keyword.Color)
	require.Equal(t, "#3e4f7a", *s.Markdown.CodeBlock.Chroma.KeywordType.Color)

	amber, err := ThemePalette("charcoal-amber")
	require.NoError(t, err)
	require.Equal(t, "#181818", amber.BgBase)
	require.NotEmpty(t, amber.DiffInsertCodeBg, "charcoal-amber's diff roles are derived")
	_, err = LoadTheme("charcoal-amber")
	require.NoError(t, err)

	dir := ThemeDirs()[0]
	require.NoError(t, os.WriteFile(filepath.Join(dir, "scholars-edition.json"), []byte(`{"base":"scholars-edition","keyword":"#123456"}`), 0o644))
	shadowed, err := LoadTheme("scholars-edition")
	require.NoError(t, err)
	require.NotEqual(t, "#0f0f0f", *shadowed.Markdown.CodeBlock.Chroma.Keyword.Color, "a shadowing file loses the override")
}

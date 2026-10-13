package model

import (
	"testing"

	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/stretchr/testify/require"
)

func TestThemeControllerSwapForProvider(t *testing.T) {
	t.Parallel()

	var c themeController
	c.key = styles.ThemeKeyForProvider("openai")
	require.False(t, c.swapForProvider("openai", false), "same theme: no rebuild")
	differs := styles.ThemeKeyForProvider("hyper") != c.key
	require.Equal(t, differs, c.swapForProvider("hyper", false))
	require.Equal(t, styles.ThemeKeyForProvider("hyper"), c.key)
	require.False(t, c.swapForProvider("openai", true), "a configured theme wins")

	c.userSelected = true
	require.False(t, c.swapForProvider("hyper", false), "a theme the user picked wins")
}

func TestThemeControllerPreview(t *testing.T) {
	t.Parallel()

	var c themeController
	_, ok := c.takePreview()
	require.False(t, ok)

	first := styles.CharmtonePantera()
	c.beginPreview(&first)
	second := styles.CharmtonePantera()
	second.Background = nil
	c.beginPreview(&second)
	saved, ok := c.takePreview()
	require.True(t, ok)
	require.Equal(t, first.Background, saved.Background, "the first preview keeps the original styles")
	_, ok = c.takePreview()
	require.False(t, ok)

	c.beginPreview(&first)
	c.endPreview()
	_, ok = c.takePreview()
	require.False(t, ok)
}

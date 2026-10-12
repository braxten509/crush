package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSavedThemeReadsGlobalConfigsInLoadOrder(t *testing.T) {
	configDir, dataDir := t.TempDir(), t.TempDir()
	t.Setenv("CRUSH_GLOBAL_CONFIG", configDir)
	t.Setenv("CRUSH_GLOBAL_DATA", dataDir)
	write := func(dir, body string) {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "crush.json"), []byte(body), 0o600))
	}

	require.Equal(t, "", SavedTheme(), "no file means the default theme")

	write(configDir, `{"options":{"tui":{"theme":"gruvbox-dark"}}}`)
	require.Equal(t, "gruvbox-dark", SavedTheme(), "the legacy name still counts")

	write(dataDir, `{"options":{"tui":{"active_theme":"graphite"}}}`)
	require.Equal(t, "graphite", SavedTheme(), "the data file, where the theme picker saves, wins")
}

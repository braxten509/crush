package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGlobalAutocompactPersistence(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "crush.json")
	store := &ConfigStore{config: &Config{Options: &Options{}}, globalDataPath: path}
	require.NoError(t, store.SetConfigField(ScopeGlobal, "options.auto_compact_token_limit", int64(275000)))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var loaded Config
	require.NoError(t, json.Unmarshal(data, &loaded))
	require.EqualValues(t, 275000, loaded.Options.GetAutoCompactTokenLimit())
	require.Empty(t, loaded.Models, "the global option must not create a model override")
	var unset *Options
	require.EqualValues(t, 400000, unset.GetAutoCompactTokenLimit())
	require.EqualValues(t, 400000, (&Options{}).GetAutoCompactTokenLimit())
}

// A setting whose file write fails is not kept in memory either, so what
// Crush shows matches what it saved.
func TestUpdateKeepsMemoryWhenTheWriteFails(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	blocker := filepath.Join(dir, "file")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0o600))
	store := &ConfigStore{config: &Config{Options: &Options{}}, globalDataPath: filepath.Join(blocker, "crush.json")}

	require.Error(t, store.SetCompactMode(ScopeGlobal, true))
	tui := store.Config().Options.TUI
	require.False(t, tui != nil && tui.CompactMode, "the failed change stays out of memory")
}

package config

import (
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"os"
	"path/filepath"
	"testing"
)

func TestSkillTogglesPersistWithoutClobberingOtherInstances(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "config.json")
	require.NoError(t, os.WriteFile(file, []byte(`{"options":{"notifications":"disabled"}}`), 0o600))
	first := testStoreWithPath(&Config{Options: &Options{}}, dir)
	second := testStoreWithPath(&Config{Options: &Options{}}, dir)
	snapshot := first.Config()
	require.NoError(t, first.SetSkillEnabled("alpha", false))
	require.Empty(t, snapshot.Options.DisabledSkills, "held snapshots remain immutable")
	require.NoError(t, second.SetSkillEnabled("beta", false))
	require.NoError(t, first.SetSkillEnabled("alpha", true))
	data, err := os.ReadFile(file)
	require.NoError(t, err)
	require.JSONEq(t, `["beta"]`, gjson.GetBytes(data, "options.disabled_skills").Raw)
	require.Equal(t, "disabled", gjson.GetBytes(data, "options.notifications").String())
	require.Equal(t, []string{"beta"}, first.Config().Options.DisabledSkills)
}

func TestFailedSkillToggleKeepsConfigSnapshot(t *testing.T) {
	dir := t.TempDir()
	blocked := filepath.Join(dir, "blocked")
	require.NoError(t, os.WriteFile(blocked, []byte("not a directory"), 0o600))
	store := testStoreWithPath(&Config{Options: &Options{}}, dir)
	store.globalDataPath = filepath.Join(blocked, "config.json")
	before := store.Config()
	require.Error(t, store.SetSkillEnabled("alpha", false))
	require.Same(t, before, store.Config())
}

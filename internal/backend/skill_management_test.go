package backend_test

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/charmbracelet/crush/internal/backend"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/skills"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestManagedSkillTogglePersistsAndUpdatesVisibleCatalog(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CRUSH_GLOBAL_CONFIG", filepath.Join(root, "config"))
	t.Setenv("CRUSH_GLOBAL_DATA", filepath.Join(root, "data"))
	t.Setenv("CRUSH_SKILLS_DIR", filepath.Join(root, "skills"))
	wd := filepath.Join(root, "project")
	writeSkill(t, wd, "managed-example", "Managed skill")
	store, err := config.Init(wd, "", false)
	require.NoError(t, err)
	b := backend.New(t.Context(), store, nil)
	clientID := uuid.NewString()
	ws, _, err := b.CreateWorkspace(proto.Workspace{ClientID: clientID, Path: wd, DataDir: filepath.Join(wd, ".crush")})
	require.NoError(t, err)
	t.Cleanup(func() { _ = b.DeleteWorkspace(ws.ID, clientID) })
	installed, err := b.InstalledSkills(ws.ID)
	require.NoError(t, err)
	index := slices.IndexFunc(installed, func(s skills.InstalledSkill) bool { return s.Name == "managed-example" })
	require.GreaterOrEqual(t, index, 0)
	skillID := installed[index].ID
	require.NoError(t, b.ManageSkill(t.Context(), ws.ID, proto.ManageSkillRequest{Action: "toggle", Name: "managed-example", Enabled: false}))
	installed, err = b.InstalledSkills(ws.ID)
	require.NoError(t, err)
	require.False(t, installed[slices.IndexFunc(installed, func(s skills.InstalledSkill) bool { return s.Name == "managed-example" })].Enabled)
	visible, err := b.ListSkills(ws.ID)
	require.NoError(t, err)
	require.False(t, slices.ContainsFunc(visible, func(s proto.SkillInfo) bool { return s.Name == "managed-example" }))
	_, _, err = b.ReadSkill(t.Context(), ws.ID, skillID)
	require.ErrorIs(t, err, skills.ErrSkillNotFound)
	require.FileExists(t, skillID, "disabling keeps the skill intact")
	require.NoError(t, ws.Cfg.ReloadFromDisk(t.Context()))
	require.Contains(t, ws.Cfg.Config().Options.DisabledSkills, "managed-example")
	require.NoError(t, b.ManageSkill(t.Context(), ws.ID, proto.ManageSkillRequest{Action: "toggle", Name: "managed-example", Enabled: true}))
	require.NoError(t, ws.Cfg.ReloadFromDisk(t.Context()))
	require.NotContains(t, ws.Cfg.Config().Options.DisabledSkills, "managed-example")
	visible, err = b.ListSkills(ws.ID)
	require.NoError(t, err)
	require.True(t, slices.ContainsFunc(visible, func(s proto.SkillInfo) bool { return s.Name == "managed-example" }))
	_, _, err = b.ReadSkill(t.Context(), ws.ID, skillID)
	require.NoError(t, err)
}

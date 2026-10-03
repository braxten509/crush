package dialog

import (
	tea "charm.land/bubbletea/v2"
	"context"
	"errors"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/skills"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/styles"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
	"testing"
)

type skillManagementStub struct {
	entries   []skills.InstalledSkill
	changes   int
	installed int
	listErr   error
}

func (s *skillManagementStub) ListInstalledSkills(context.Context) ([]skills.InstalledSkill, error) {
	return s.entries, s.listErr
}

func TestSuccessfulSkillMutationStillRefreshesCommandsAfterListFailure(t *testing.T) {
	sty := styles.CharmtonePantera()
	manager := &skillManagementStub{entries: []skills.InstalledSkill{{CatalogEntry: skills.CatalogEntry{ID: "example", Name: "example"}, Enabled: true}}}
	d := NewSkills(&common.Common{Styles: &sty}, manager)
	d.HandleMsg(d.Load()())
	manager.listErr = errors.New("listing failed")
	action := d.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEnter}).(ActionCmd)
	msg := action.Cmd().(SkillsResultMsg)
	require.True(t, msg.Mutated)
	require.Error(t, msg.Err)
	require.IsType(t, ActionSkillsChanged{}, d.HandleMsg(msg))
}
func (s *skillManagementStub) SetSkillEnabled(_ context.Context, name string, enabled bool) error {
	s.changes++
	for i := range s.entries {
		if s.entries[i].Name == name {
			s.entries[i].Enabled = enabled
		}
	}
	return nil
}
func (s *skillManagementStub) InstallSkill(context.Context, skills.DirectorySkill) error {
	s.installed++
	return nil
}

func TestSkillsCommandAndPersistedToggle(t *testing.T) {
	sty := styles.CharmtonePantera()
	com := &common.Common{Styles: &sty, Workspace: fastModeWorkspace{cfg: &config.Config{}}}
	c, err := NewCommands(com, "", false, false, false, nil, nil)
	require.NoError(t, err)
	for _, r := range "skills" {
		c.HandleMsg(keyMsg(r))
	}
	require.Equal(t, ActionOpenDialog{SkillsID}, c.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEnter}))
	manager := &skillManagementStub{entries: []skills.InstalledSkill{{CatalogEntry: skills.CatalogEntry{ID: "example", Name: "example", Description: "An example"}, Enabled: true}}}
	d := NewSkills(com, manager)
	d.HandleMsg(d.Load()())
	require.Len(t, d.list.FilteredItems(), 1)
	require.Equal(t, "Enabled", d.list.SelectedItem().(*CommandItem).Shortcut())
	action := d.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEnter}).(ActionCmd)
	require.Zero(t, manager.changes, "the render loop never persists settings synchronously")
	require.True(t, d.busy)
	require.Nil(t, d.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEnter}), "repeat Enter cannot race a toggle")
	require.IsType(t, ActionSkillsChanged{}, d.HandleMsg(action.Cmd()))
	require.Equal(t, "Disabled", d.list.SelectedItem().(*CommandItem).Shortcut())
	reopened := NewSkills(com, manager)
	reopened.HandleMsg(reopened.Load()())
	require.Equal(t, "Disabled", reopened.list.SelectedItem().(*CommandItem).Shortcut())
}

func TestSkillsSearchDoesNotInstallOldResultsAfterQueryChanges(t *testing.T) {
	sty := styles.CharmtonePantera()
	d := NewSkills(&common.Common{Styles: &sty}, &skillManagementStub{})
	d.HandleMsg(tea.KeyPressMsg{Code: tea.KeyTab})
	d.input.SetValue("react")
	d.HandleMsg(SkillsResultMsg{Dialog: d, Operation: "search", Query: "react", Results: []skills.DirectorySkill{{ID: "owner/repo/example", Name: "example", Source: "owner/repo", SkillID: "example"}}})
	d.input.SetValue("python")
	action := d.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.IsType(t, ActionCmd{}, action)
	require.Equal(t, "Searching skills.sh…", d.status, "changed queries search before any install")
	d.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEscape})
	require.ErrorIs(t, action.(ActionCmd).Cmd().(SkillsResultMsg).Err, context.Canceled)
}

func TestSkillsRenderingAtWideAndNarrowSizes(t *testing.T) {
	sty := styles.CharmtonePantera()
	manager := &skillManagementStub{entries: []skills.InstalledSkill{
		{CatalogEntry: skills.CatalogEntry{ID: "first", Name: "first", Description: "First skill"}, Enabled: true},
		{CatalogEntry: skills.CatalogEntry{ID: "second", Name: "second", Description: "Second skill"}},
	}}
	d := NewSkills(&common.Common{Styles: &sty}, manager)
	d.HandleMsg(d.Load()())
	for _, size := range [][2]int{{100, 30}, {40, 20}} {
		screen := uv.NewScreenBuffer(size[0], size[1])
		d.Draw(screen, screen.Bounds())
		out := ansi.Strip(screen.Render())
		t.Log("\n" + out)
		require.Contains(t, out, "Skills")
		require.Contains(t, out, "Enabled")
		require.Contains(t, out, "Disabled")
		require.Contains(t, out, "first")
		require.Contains(t, out, "second")
	}
	d.HandleMsg(tea.KeyPressMsg{Code: tea.KeyTab})
	d.input.SetValue("sql")
	d.HandleMsg(SkillsResultMsg{Dialog: d, Operation: "search", Query: "sql", Results: []skills.DirectorySkill{{ID: "owner/repo/sql", Name: "sql", Source: "owner/repo", SkillID: "sql", Installs: 1024}}})
	screen := uv.NewScreenBuffer(100, 30)
	d.Draw(screen, screen.Bounds())
	out := ansi.Strip(screen.Render())
	t.Log("\n" + out)
	require.Contains(t, out, "Browse skills.sh")
	require.Contains(t, out, "1024 installs")
	require.Contains(t, out, "owner/repo")
}

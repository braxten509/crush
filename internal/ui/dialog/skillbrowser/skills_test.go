package skillbrowser

import (
	"context"
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/skills"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/dialog"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/stretchr/testify/require"
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
	action := d.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEnter}).(dialog.ActionCmd)
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

// TestSkillsPersistedToggle pins that toggling a skill persists off the
// render loop and survives reopening the dialog.
func TestSkillsPersistedToggle(t *testing.T) {
	sty := styles.CharmtonePantera()
	com := &common.Common{Styles: &sty}
	manager := &skillManagementStub{entries: []skills.InstalledSkill{{CatalogEntry: skills.CatalogEntry{ID: "example", Name: "example", Description: "An example"}, Enabled: true}}}
	d := NewSkills(com, manager)
	d.HandleMsg(d.Load()())
	require.Len(t, d.list.FilteredItems(), 1)
	before := d.list.SelectedItem().(*dialog.CommandItem).Shortcut()
	action := d.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEnter}).(dialog.ActionCmd)
	require.Zero(t, manager.changes, "the render loop never persists settings synchronously")
	require.True(t, d.busy)
	require.Nil(t, d.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEnter}), "repeat Enter cannot race a toggle")
	require.IsType(t, ActionSkillsChanged{}, d.HandleMsg(action.Cmd()))
	require.Equal(t, 1, manager.changes)
	require.False(t, manager.entries[0].Enabled)
	toggled := d.list.SelectedItem().(*dialog.CommandItem).Shortcut()
	require.NotEqual(t, before, toggled)
	reopened := NewSkills(com, manager)
	reopened.HandleMsg(reopened.Load()())
	require.Equal(t, toggled, reopened.list.SelectedItem().(*dialog.CommandItem).Shortcut())
}

func TestSkillsSearchDoesNotInstallOldResultsAfterQueryChanges(t *testing.T) {
	sty := styles.CharmtonePantera()
	manager := &skillManagementStub{}
	d := NewSkills(&common.Common{Styles: &sty}, manager)
	d.HandleMsg(tea.KeyPressMsg{Code: tea.KeyTab})
	d.input.SetValue("react")
	d.HandleMsg(SkillsResultMsg{Dialog: d, Operation: "search", Query: "react", Results: []skills.DirectorySkill{{ID: "owner/repo/example", Name: "example", Source: "owner/repo", SkillID: "example"}}})
	d.input.SetValue("python")
	action := d.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.IsType(t, dialog.ActionCmd{}, action)
	d.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEscape})
	require.ErrorIs(t, action.(dialog.ActionCmd).Cmd().(SkillsResultMsg).Err, context.Canceled)
	require.Zero(t, manager.installed, "changed queries search before any install")
}

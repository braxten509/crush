package workspace

import (
	"context"
	"github.com/charmbracelet/crush/internal/skills"
)

var _ skills.Management = (*AppWorkspace)(nil)
var _ skills.Management = (*ClientWorkspace)(nil)

func (w *AppWorkspace) ListInstalledSkills(_ context.Context) ([]skills.InstalledSkill, error) {
	return skills.Installed(w.store, w.app.Skills), nil
}
func (w *AppWorkspace) SetSkillEnabled(_ context.Context, name string, enabled bool) error {
	return skills.SetEnabled(w.store, w.app.Skills, name, enabled)
}
func (w *AppWorkspace) InstallSkill(ctx context.Context, selected skills.DirectorySkill) error {
	return skills.Install(ctx, w.store, w.app.Skills, selected)
}
func (w *ClientWorkspace) ListInstalledSkills(ctx context.Context) ([]skills.InstalledSkill, error) {
	return w.client.InstalledSkills(ctx, w.workspaceID())
}
func (w *ClientWorkspace) SetSkillEnabled(ctx context.Context, name string, enabled bool) error {
	return w.client.ManageSkill(ctx, w.workspaceID(), "toggle", name, enabled, skills.DirectorySkill{})
}
func (w *ClientWorkspace) InstallSkill(ctx context.Context, selected skills.DirectorySkill) error {
	return w.client.ManageSkill(ctx, w.workspaceID(), "install", "", false, selected)
}

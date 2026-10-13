package workspace

import (
	"context"

	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/skills"
)

func (w *AppWorkspace) ListInstalledSkills(_ context.Context) ([]skills.InstalledSkill, error) {
	return w.ops.InstalledSkills(), nil
}

func (w *AppWorkspace) SetSkillEnabled(_ context.Context, name string, enabled bool) error {
	return w.ops.SetSkillEnabled(name, enabled)
}

func (w *AppWorkspace) InstallSkill(ctx context.Context, selected skills.DirectorySkill) error {
	return w.ops.InstallSkill(ctx, selected)
}

func (w *ClientWorkspace) ListInstalledSkills(ctx context.Context) ([]skills.InstalledSkill, error) {
	return w.client.InstalledSkills(ctx, w.workspaceID())
}

func (w *ClientWorkspace) SetSkillEnabled(ctx context.Context, name string, enabled bool) error {
	return w.client.ManageSkill(ctx, w.workspaceID(), proto.ManageSkillToggle, name, enabled, skills.DirectorySkill{})
}

func (w *ClientWorkspace) InstallSkill(ctx context.Context, selected skills.DirectorySkill) error {
	return w.client.ManageSkill(ctx, w.workspaceID(), proto.ManageSkillInstall, "", false, selected)
}

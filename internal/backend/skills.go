package backend

import (
	"context"
	"fmt"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/skills"
)

func (b *Backend) InstalledSkills(id string) ([]skills.InstalledSkill, error) {
	ws, err := b.GetWorkspace(id)
	if err != nil {
		return nil, err
	}
	return skills.Installed(ws.Cfg, ws.Skills), nil
}

func (b *Backend) ManageSkill(ctx context.Context, id string, req proto.ManageSkillRequest) error {
	ws, err := b.GetWorkspace(id)
	if err != nil {
		return err
	}
	switch req.Action {
	case "toggle":
		err = skills.SetEnabled(ws.Cfg, ws.Skills, req.Name, req.Enabled)
	case "install":
		err = skills.Install(ctx, ws.Cfg, ws.Skills, req.Skill)
	default:
		return fmt.Errorf("unknown skill action %q", req.Action)
	}
	if err == nil {
		publishConfigChanged(ws)
	}
	return err
}

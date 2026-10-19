package backend

import (
	"log/slog"

	"github.com/charmbracelet/crush/internal/commands"
	"github.com/charmbracelet/crush/internal/proto"
)

// CustomCommands lists the custom commands and the user-invocable skills,
// as the terminal's palette loads them (internal/ui/model/commands.go;
// G-7).
func (b *Backend) CustomCommands(workspaceID string) ([]proto.CustomCommand, error) {
	o, err := b.ops(workspaceID)
	if err != nil {
		return nil, err
	}
	cmds, err := commands.LoadCustomCommands(o.Cfg.Config())
	if err != nil {
		slog.Error("Failed to load custom commands", "error", err)
	}
	cmds = append(cmds, commands.FromSkillCatalog(o.ListSkills())...)
	return CustomCommandsToProto(cmds), nil
}

// CustomCommandsToProto converts palette commands to their wire form.
func CustomCommandsToProto(cmds []commands.CustomCommand) []proto.CustomCommand {
	out := make([]proto.CustomCommand, 0, len(cmds))
	for _, c := range cmds {
		wire := proto.CustomCommand{ID: c.ID, Name: c.Name, Content: c.Content, Arguments: []proto.CustomCommandArgument{}}
		for _, a := range c.Arguments {
			wire.Arguments = append(wire.Arguments, proto.CustomCommandArgument{Name: a.ID, Title: a.Title, Required: a.Required})
		}
		if c.Skill != nil {
			wire.Skill = &proto.CustomCommandSkill{
				Name:          c.Skill.Name,
				Description:   c.Skill.Description,
				SkillFilePath: c.Skill.SkillFilePath,
			}
		}
		out = append(out, wire)
	}
	return out
}

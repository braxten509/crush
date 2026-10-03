package proto

import "github.com/charmbracelet/crush/internal/skills"

type ManageSkillRequest struct {
	Action  string                `json:"action"`
	Name    string                `json:"name,omitempty"`
	Enabled bool                  `json:"enabled"`
	Skill   skills.DirectorySkill `json:"skill"`
}

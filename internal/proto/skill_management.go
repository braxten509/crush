package proto

import "github.com/charmbracelet/crush/internal/skills"

// Skill management actions carried in [ManageSkillRequest.Action].
const (
	// ManageSkillToggle enables or disables an installed skill.
	ManageSkillToggle = "toggle"
	// ManageSkillInstall installs a skill from a directory listing.
	ManageSkillInstall = "install"
)

// ManageSkillRequest installs a skill or toggles an installed one.
type ManageSkillRequest struct {
	Action  string                `json:"action"`
	Name    string                `json:"name,omitempty"`
	Enabled bool                  `json:"enabled"`
	Skill   skills.DirectorySkill `json:"skill"`
}

package agent

import (
	"github.com/charmbracelet/crush/internal/skills"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestSkillDirectoryContainsDiscoveryMetadataOnly(t *testing.T) {
	t.Parallel()
	policy := formatSkillPolicy([]string{"disabled"}, []*skills.Skill{
		{Name: "chosen", Description: "Use for the sample task", SkillFilePath: "/skills/chosen/SKILL.md", Instructions: "FULL PROCEDURE MUST STAY OUT OF DIRECTORY"},
		{Name: "manual", Description: "Manual only", DisableModelInvocation: true},
	})
	require.Contains(t, policy, "chosen")
	require.Contains(t, policy, "Use for the sample task")
	require.Contains(t, policy, "/skills/chosen/SKILL.md")
	require.Contains(t, policy, "read its SKILL.md")
	require.NotContains(t, policy, "FULL PROCEDURE")
	require.NotContains(t, policy, "Manual only")
	require.Contains(t, policy, `["disabled"]`)
}

func TestPerTurnSkillPolicyReplacesStaleAvailability(t *testing.T) {
	prompt := "Keep Git guards active.\n<available_skills>old-disabled-skill</available_skills>\nUse the shared memory."
	policy := "<crush_skill_settings>Disabled skill names: [\"old-disabled-skill\"]\n<available_skills>new-enabled-skill</available_skills></crush_skill_settings>"
	result := withCurrentSkillPolicy(prompt, policy)
	require.Contains(t, result, "Keep Git guards active.")
	require.Contains(t, result, "Use the shared memory.")
	require.NotContains(t, result, "<available_skills>old-disabled-skill")
	require.Contains(t, result, "<available_skills>new-enabled-skill")
	require.Contains(t, result, "Disabled skill names")
}

func TestSkillPolicyDoesNotInvokeCatalogExamples(t *testing.T) {
	t.Parallel()
	policy := formatSkillPolicy(nil, []*skills.Skill{
		{Name: "align", Description: "Use for $align and #align", SkillFilePath: "/skills/align/SKILL.md"},
		{Name: "prompter", Description: "Use for $prompter or /prompter"},
		{Name: "teach", Description: "Use for $teach"},
	})
	require.NotContains(t, policy, "$align")
	require.NotContains(t, policy, "$prompter")
	require.NotContains(t, policy, "$teach")
	require.Contains(t, policy, "not a user request or skill invocation")
	// Refreshing the catalog must not disable a real command typed by the user.
	prompt := withCurrentSkillPolicy("$teach SQL joins", policy)
	require.Contains(t, prompt, "$teach SQL joins")
}

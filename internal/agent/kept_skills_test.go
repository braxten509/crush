package agent

import (
	"strings"
	"testing"

	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/skills"
	"github.com/stretchr/testify/require"
)

func keptTestSkill(name string) *skills.Skill {
	return &skills.Skill{
		Name:          name,
		Instructions:  "\n  Follow the " + name + " steps.\n",
		SkillFilePath: "/skills/" + name + "/SKILL.md",
	}
}

func TestAPIKeptSkillsTextHasEveryFullText(t *testing.T) {
	t.Parallel()

	require.Empty(t, apiKeptSkillsText(nil))
	text := apiKeptSkillsText([]*skills.Skill{keptTestSkill("alpha"), keptTestSkill("beta")})
	require.True(t, strings.HasPrefix(text, "<crush_kept_skills>\n"+keptSkillsIntro))
	require.True(t, strings.HasSuffix(text, "</crush_kept_skills>"))
	require.Contains(t, text, "<skill name=\"alpha\" location=\"/skills/alpha/SKILL.md\">\nFollow the alpha steps.\n</skill>")
	require.Contains(t, text, "Follow the beta steps.")
}

func TestCLIKeptSkillsTextSendsFullTextOnceThenReminds(t *testing.T) {
	t.Parallel()

	sessionID := t.Name()
	t.Cleanup(func() { forgetKeptSkillsSent(sessionID) })
	alpha := keptTestSkill("alpha")

	first := cliKeptSkillsText(sessionID, "claude", []*skills.Skill{alpha}, nil, false)
	require.Contains(t, first, "Follow the alpha steps.")

	second := cliKeptSkillsText(sessionID, "claude", []*skills.Skill{alpha}, nil, false)
	require.NotContains(t, second, "Follow the alpha steps.")
	require.Contains(t, second, "- alpha is on (/skills/alpha/SKILL.md).")
	require.Contains(t, second, keptSkillsIntro)

	// A skill turned on later gets its full text while the old one stays a
	// reminder.
	beta := keptTestSkill("beta")
	third := cliKeptSkillsText(sessionID, "claude", []*skills.Skill{alpha, beta}, nil, false)
	require.NotContains(t, third, "Follow the alpha steps.")
	require.Contains(t, third, "Follow the beta steps.")

	// Another agent CLI in the same chat has not seen it yet.
	other := cliKeptSkillsText(sessionID, "codex", []*skills.Skill{alpha}, nil, false)
	require.Contains(t, other, "Follow the alpha steps.")
}

func TestCLIKeptSkillsTextResendsAfterCompactionOrFreshSession(t *testing.T) {
	t.Parallel()

	sessionID := t.Name()
	t.Cleanup(func() { forgetKeptSkillsSent(sessionID) })
	kept := []*skills.Skill{keptTestSkill("alpha")}

	cliKeptSkillsText(sessionID, "claude", kept, nil, false)
	cliKeptSkillsText(sessionID, "codex", kept, nil, false)
	forgetKeptSkillsSent(sessionID)
	require.Contains(t, cliKeptSkillsText(sessionID, "claude", kept, nil, false), "Follow the alpha steps.")
	require.Contains(t, cliKeptSkillsText(sessionID, "codex", kept, nil, false), "Follow the alpha steps.")

	// A fresh native session has none of the earlier history.
	require.NotContains(t, cliKeptSkillsText(sessionID, "claude", kept, nil, false), "Follow the alpha steps.")
	require.Contains(t, cliKeptSkillsText(sessionID, "claude", kept, nil, true), "Follow the alpha steps.")
}

func TestForgetKeptSkillsSentLeavesOtherChats(t *testing.T) {
	t.Parallel()

	chat, other := t.Name()+"-chat", t.Name()+"-other"
	t.Cleanup(func() { forgetKeptSkillsSent(chat); forgetKeptSkillsSent(other) })
	kept := []*skills.Skill{keptTestSkill("alpha")}

	cliKeptSkillsText(chat, "claude", kept, nil, false)
	cliKeptSkillsText(other, "claude", kept, nil, false)
	forgetKeptSkillsSent(chat)
	require.NotContains(t, cliKeptSkillsText(other, "claude", kept, nil, false), "Follow the alpha steps.")
}

func TestCLIKeptSkillsTextEmptyWithoutSkills(t *testing.T) {
	t.Parallel()
	require.Empty(t, cliKeptSkillsText(t.Name(), "claude", nil, nil, true))
}

func TestCLIKeptSkillsTextSaysWhenASkillIsTurnedOff(t *testing.T) {
	t.Parallel()

	sessionID := t.Name()
	t.Cleanup(func() { forgetKeptSkillsSent(sessionID) })
	alpha, beta := keptTestSkill("alpha"), keptTestSkill("beta")

	cliKeptSkillsText(sessionID, "claude", []*skills.Skill{alpha, beta}, nil, false)
	text := cliKeptSkillsText(sessionID, "claude", []*skills.Skill{beta}, nil, false)
	require.Contains(t, text, "- alpha is off for this chat now. Stop following it.")
	require.Contains(t, text, "- beta is on")

	// The off line is said once; with nothing on, nothing more is sent.
	cliKeptSkillsText(sessionID, "claude", nil, nil, false)
	require.Empty(t, cliKeptSkillsText(sessionID, "claude", nil, nil, false))
}

func TestCLIKeptSkillsTextSaysOffAfterCompaction(t *testing.T) {
	t.Parallel()

	sessionID := t.Name()
	t.Cleanup(func() { forgetKeptSkillsSent(sessionID) })
	cliKeptSkillsText(sessionID, "claude", []*skills.Skill{keptTestSkill("alpha")}, nil, false)
	forgetKeptSkillsSent(sessionID)

	text := cliKeptSkillsText(sessionID, "claude", nil, nil, false)
	require.Contains(t, text, "alpha is off for this chat now")
	require.NotContains(t, text, keptSkillsIntro)
}

func TestCLIKeptSkillsTextFreshSessionSkipsOffLines(t *testing.T) {
	t.Parallel()

	sessionID := t.Name()
	t.Cleanup(func() { forgetKeptSkillsSent(sessionID) })
	cliKeptSkillsText(sessionID, "claude", []*skills.Skill{keptTestSkill("alpha")}, nil, false)
	require.Empty(t, cliKeptSkillsText(sessionID, "claude", nil, nil, true), "a fresh native session never saw the skill")
}

func TestCLIKeptSkillsTextTellsAResumedCLIAfterARestart(t *testing.T) {
	t.Parallel()

	// No record for this chat, as after a Crush restart: the saved list of
	// turned-off skills is used once.
	sessionID := t.Name()
	t.Cleanup(func() { forgetKeptSkillsSent(sessionID) })
	calls := 0
	turnedOff := func() []session.KeptSkill {
		calls++
		return []session.KeptSkill{{ID: "/skills/alpha/SKILL.md", Name: "alpha"}}
	}

	text := cliKeptSkillsText(sessionID, "claude", nil, turnedOff, false)
	require.Contains(t, text, "alpha is off for this chat now")
	require.Empty(t, cliKeptSkillsText(sessionID, "claude", nil, turnedOff, false))
	require.Equal(t, 1, calls, "the saved list is only read when there is no record")

	// A fresh native session never saw it, so it is not read or told.
	require.Empty(t, cliKeptSkillsText(t.Name()+"-fresh", "claude", nil, turnedOff, true))
	require.Equal(t, 1, calls)
}

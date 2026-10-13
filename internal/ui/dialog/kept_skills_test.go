package dialog

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

func newKeptSkillsForTest(items []KeptSkillItem) *KeptSkills {
	s := styles.CharmtonePantera()
	com := &common.Common{Styles: &s}
	return NewKeptSkills(com, items)
}

func typeKeys(m *KeptSkills, text string) {
	for _, r := range text {
		m.HandleMsg(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

func TestKeptSkills_EnterTurnsSkillOnAndOff(t *testing.T) {
	t.Parallel()

	m := newKeptSkillsForTest([]KeptSkillItem{
		{ID: "/a/SKILL.md", Name: "align"},
		{ID: "/d/SKILL.md", Name: "deep-debugging"},
	})

	require.Nil(t, m.HandleMsg(tea.KeyPressMsg{Code: tea.KeyDown}))
	action := m.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.Equal(t, ActionToggleKeptSkill{ID: "/d/SKILL.md", Name: "deep-debugging", On: true}, action)
	require.True(t, m.Items()[1].On)

	action = m.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.Equal(t, ActionToggleKeptSkill{ID: "/d/SKILL.md", Name: "deep-debugging", On: false}, action)
	require.False(t, m.Items()[1].On)
}

func TestKeptSkills_FilterNarrowsTheList(t *testing.T) {
	t.Parallel()

	m := newKeptSkillsForTest([]KeptSkillItem{
		{ID: "/a/SKILL.md", Name: "align"},
		{ID: "/d/SKILL.md", Name: "deep-debugging"},
		{ID: "/t/SKILL.md", Name: "teach"},
	})

	typeKeys(m, "DEEP")
	action := m.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.Equal(t, ActionToggleKeptSkill{ID: "/d/SKILL.md", Name: "deep-debugging", On: true}, action)

	typeKeys(m, "zzz")
	require.Nil(t, m.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEnter}), "nothing to toggle when nothing matches")
}

func TestKeptSkills_ScrollsToTheCursor(t *testing.T) {
	t.Parallel()

	items := make([]KeptSkillItem, 30)
	for i := range items {
		items[i] = KeptSkillItem{ID: fmt.Sprintf("/s%02d/SKILL.md", i), Name: fmt.Sprintf("skill-%02d", i)}
	}
	m := newKeptSkillsForTest(items)
	for range 20 {
		m.HandleMsg(tea.KeyPressMsg{Code: tea.KeyDown})
	}

	view := ansi.Strip(m.listView(60, 5))
	require.Contains(t, view, "skill-20")
	require.NotContains(t, view, "skill-00")
	require.Contains(t, view, "↓ 9 more")
	require.Equal(t, 5, strings.Count(view, "skill-"))
}

func TestKeptSkills_ShowsOnAndOff(t *testing.T) {
	t.Parallel()

	m := newKeptSkillsForTest([]KeptSkillItem{
		{ID: "/a/SKILL.md", Name: "align"},
		{ID: "/d/SKILL.md", Name: "deep-debugging", On: true},
	})
	lines := strings.Split(ansi.Strip(m.listView(60, 10)), "\n")
	var align, deep string
	for _, line := range lines {
		switch {
		case strings.Contains(line, "align"):
			align = line
		case strings.Contains(line, "deep-debugging"):
			deep = line
		}
	}
	require.True(t, strings.HasSuffix(strings.TrimRight(align, " "), "off"))
	require.True(t, strings.HasSuffix(strings.TrimRight(deep, " "), "on"))

	m.SetOn("/d/SKILL.md", false)
	require.False(t, m.Items()[1].On)
}

func TestKeptSkills_EscCloses(t *testing.T) {
	t.Parallel()

	m := newKeptSkillsForTest(nil)
	require.Equal(t, ActionClose{}, m.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEscape}))
	require.Contains(t, ansi.Strip(m.listView(60, 5)), "No skills found.")
}

func TestKeptSkillsCommandIsFoundWithoutTakingOverSkills(t *testing.T) {
	sty := styles.CharmtonePantera()
	com := &common.Common{Styles: &sty, Workspace: fastModeWorkspace{cfg: &config.Config{}}}
	c, err := NewCommands(com, "", false, false, false, nil, nil)
	require.NoError(t, err)
	for _, r := range "keep skills" {
		c.HandleMsg(keyMsg(r))
	}
	require.Equal(t, ActionOpenDialog{KeptSkillsID}, c.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEnter}))
}

// Skills on for the chat are grouped by who turned them on; turning a skill
// on yourself makes it yours, and the cursor follows it to its new group.
func TestKeptSkills_GroupsByWhoTurnedThemOn(t *testing.T) {
	t.Parallel()

	m := newKeptSkillsForTest([]KeptSkillItem{
		{ID: "/a/SKILL.md", Name: "align"},
		{ID: "/d/SKILL.md", Name: "deep-debugging", On: true},
		{ID: "/t/SKILL.md", Name: "teach", On: true, ByAgent: true},
	})
	order := func() []string {
		var out []string
		for _, line := range strings.Split(ansi.Strip(m.listView(60, 10)), "\n") {
			for _, want := range []string{"You turned on", "Agent turned on", "Off", "align", "deep-debugging", "teach"} {
				if strings.Contains(line, want) {
					out = append(out, want)
					break
				}
			}
		}
		return out
	}
	require.Equal(t, []string{"You turned on", "deep-debugging", "Agent turned on", "teach", "Off", "align"}, order())

	m.HandleMsg(tea.KeyPressMsg{Code: tea.KeyDown})
	m.HandleMsg(tea.KeyPressMsg{Code: tea.KeyDown})
	action := m.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.Equal(t, ActionToggleKeptSkill{ID: "/a/SKILL.md", Name: "align", On: true}, action)
	require.Equal(t, []string{"You turned on", "align", "deep-debugging", "Agent turned on", "teach"}, order())
	require.Equal(t, ActionToggleKeptSkill{ID: "/a/SKILL.md", Name: "align", On: false}, m.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEnter}), "the cursor stays on the skill")

	m.SetState("/t/SKILL.md", true, false)
	require.Equal(t, []string{"You turned on", "deep-debugging", "teach", "Off", "align"}, order(), "the open list follows changes made elsewhere")
}

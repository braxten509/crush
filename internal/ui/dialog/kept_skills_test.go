package dialog

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
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

func TestKeptSkills_EnterGoesToggleableThenOnThenOff(t *testing.T) {
	t.Parallel()

	m := newKeptSkillsForTest([]KeptSkillItem{
		{ID: "/a/SKILL.md", Name: "align"},
		{ID: "/d/SKILL.md", Name: "deep-debugging"},
	})

	require.Nil(t, m.HandleMsg(tea.KeyPressMsg{Code: tea.KeyDown}))
	action := m.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.Equal(t, ActionToggleKeptSkill{ID: "/d/SKILL.md", Name: "deep-debugging", On: true, Toggleable: true}, action)
	require.Equal(t, "toggleable", m.Items()[1].state())

	action = m.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.Equal(t, ActionToggleKeptSkill{ID: "/d/SKILL.md", Name: "deep-debugging", On: true}, action)
	require.Equal(t, "on", m.Items()[1].state())

	action = m.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.Equal(t, ActionToggleKeptSkill{ID: "/d/SKILL.md", Name: "deep-debugging", On: false}, action)
	require.Equal(t, "off", m.Items()[1].state())

	m.SetOn("/d/SKILL.md", true)
	require.True(t, m.Items()[1].On)
	m.SetOn("/d/SKILL.md", false)
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
	require.Equal(t, ActionToggleKeptSkill{ID: "/d/SKILL.md", Name: "deep-debugging", On: true, Toggleable: true}, action)

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
	require.Equal(t, 5, strings.Count(view, "skill-"))
}

func TestKeptSkills_EscCloses(t *testing.T) {
	t.Parallel()

	m := newKeptSkillsForTest(nil)
	require.Equal(t, ActionClose{}, m.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEscape}))
}

// Skills on for the chat are grouped into plain on and toggleable, and the
// cursor follows a skill to its new group.
func TestKeptSkills_GroupsOnToggleableAndOff(t *testing.T) {
	t.Parallel()

	m := newKeptSkillsForTest([]KeptSkillItem{
		{ID: "/a/SKILL.md", Name: "align"},
		{ID: "/d/SKILL.md", Name: "deep-debugging", On: true},
		{ID: "/t/SKILL.md", Name: "teach", On: true, Toggleable: true},
	})
	order := func() []string {
		var out []string
		for _, line := range strings.Split(ansi.Strip(m.listView(60, 10)), "\n") {
			for _, want := range []string{"align", "deep-debugging", "teach"} {
				if strings.Contains(line, want) {
					out = append(out, want)
					break
				}
			}
		}
		return out
	}
	require.Equal(t, []string{"deep-debugging", "teach", "align"}, order())

	m.HandleMsg(tea.KeyPressMsg{Code: tea.KeyDown})
	m.HandleMsg(tea.KeyPressMsg{Code: tea.KeyDown})
	action := m.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.Equal(t, "/a/SKILL.md", action.(ActionToggleKeptSkill).ID)
	require.Equal(t, []string{"deep-debugging", "align", "teach"}, order())
	action = m.HandleMsg(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.Equal(t, "/a/SKILL.md", action.(ActionToggleKeptSkill).ID, "the cursor stays on the skill")
	require.Equal(t, []string{"align", "deep-debugging", "teach"}, order())

	m.SetState("/t/SKILL.md", false, false)
	require.Equal(t, "off", m.Items()[2].state())
	require.Equal(t, []string{"align", "deep-debugging", "teach"}, order(), "the open list follows changes made elsewhere")
}

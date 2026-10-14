package model

import (
	"context"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/skills"
	"github.com/charmbracelet/crush/internal/ui/dialog"
	"github.com/charmbracelet/crush/internal/ui/util"
)

// attachSkill reads a skill's content by ID and returns it as a markdown
// attachment to be added to the attachment toolbar. The user can then
// compose a message and send it with the skill attached.
// The name parameter is used as a fallback when the server does not
// return one.
func (m *UI) attachSkill(skillID, name string) tea.Cmd {
	ws := m.com.Workspace
	return func() tea.Msg {
		content, result, err := ws.ReadSkill(context.Background(), skillID)
		if err != nil {
			return util.NewErrorMsg(err)
		}
		fileName := result.Name
		if fileName == "" {
			fileName = name
		}
		return message.Attachment{
			FilePath: fileName,
			FileName: fileName,
			MimeType: "text/markdown",
			Content:  content,
		}
	}
}

// keptSkillsChangedMsg carries the chat after a kept skill was turned on
// or off.
type keptSkillsChangedMsg struct {
	session session.Session
	err     error
}

// keptSkills is the chat's kept skills, or the pending ones before the chat
// exists.
func (m *UI) keptSkills() []session.KeptSkill {
	if m.hasSession() {
		return m.session.KeptSkills
	}
	return m.pendingKeptSkills
}

// keptSkillsListMsg carries the skill list for the "Toggle skills"
// dialog.
type keptSkillsListMsg struct {
	entries []skills.CatalogEntry
	err     error
}

// openKeptSkillsDialog loads the skills off the UI goroutine, then opens the
// "Toggle skills" dialog.
func (m *UI) openKeptSkillsDialog() tea.Cmd {
	if m.dialog.ContainsDialog(dialog.KeptSkillsID) {
		m.dialog.BringToFront(dialog.KeptSkillsID)
		return nil
	}
	ws := m.com.Workspace
	return func() tea.Msg {
		entries, err := ws.ListSkills(context.Background())
		return keptSkillsListMsg{entries: entries, err: err}
	}
}

// keptSkillItems lists every skill, sorted by name, marking the ones that
// are on for this chat.
func (m *UI) keptSkillItems(entries []skills.CatalogEntry) []dialog.KeptSkillItem {
	kept := make(map[string]session.KeptSkill)
	for _, skill := range m.keptSkills() {
		kept[skill.ID] = skill
	}
	items := make([]dialog.KeptSkillItem, 0, len(entries))
	for _, entry := range entries {
		skill, on := kept[entry.ID]
		items = append(items, dialog.KeptSkillItem{ID: entry.ID, Name: entry.Name, On: on, Toggleable: skill.ByAgent})
	}
	slices.SortStableFunc(items, func(a, b dialog.KeptSkillItem) int {
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	return items
}

func (m *UI) setKeptSkill(skill session.KeptSkill, on bool) tea.Cmd {
	m.frame.invalidate()
	if !m.hasSession() {
		m.pendingKeptSkills = slices.DeleteFunc(m.pendingKeptSkills, func(k session.KeptSkill) bool { return k.ID == skill.ID })
		if on {
			m.pendingKeptSkills = append(m.pendingKeptSkills, skill)
		}
		return nil
	}
	ws, sessionID := m.com.Workspace, m.session.ID
	return func() tea.Msg {
		sess, err := ws.SetKeptSkill(context.Background(), sessionID, skill, on)
		return keptSkillsChangedMsg{session: sess, err: err}
	}
}

// syncKeptSkillsDialog makes an open "Toggle skills" dialog follow
// the chat's skills, such as ones the agent turned on or off.
func (m *UI) syncKeptSkillsDialog() {
	dia, ok := m.dialog.Dialog(dialog.KeptSkillsID).(*dialog.KeptSkills)
	if !ok {
		return
	}
	kept := make(map[string]session.KeptSkill)
	for _, skill := range m.keptSkills() {
		kept[skill.ID] = skill
	}
	for _, item := range dia.Items() {
		skill, on := kept[item.ID]
		if item.On != on || item.Toggleable != skill.ByAgent {
			dia.SetState(item.ID, on, skill.ByAgent)
		}
	}
}

// turnOffKeptSkill turns off a kept skill from its tag under the composer.
func (m *UI) turnOffKeptSkill(id string) tea.Cmd {
	if dia, ok := m.dialog.Dialog(dialog.KeptSkillsID).(*dialog.KeptSkills); ok {
		dia.SetOn(id, false)
	}
	return m.setKeptSkill(session.KeptSkill{ID: id}, false)
}

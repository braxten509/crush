package dialog

import (
	"slices"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/dialog/internal/kit"
	uv "github.com/charmbracelet/ultraviolet"
)

// KeptSkillsID is the identifier for the "Skills for this chat" dialog.
const KeptSkillsID = "kept_skills"

// keptSkillsMaxRows caps the visible rows; the rest scroll.
const keptSkillsMaxRows = 12

// keptSkillsNote explains what turning a skill on does.
const keptSkillsNote = "On skills are reminded every message and come back after compression."

// KeptSkillItem is one skill in the "Skills for this chat" dialog.
type KeptSkillItem struct {
	ID   string
	Name string
	On   bool
	// ByAgent is set when the agent turned the skill on.
	ByAgent bool
}

// keptSkillGroups name the list's groups, in order; see group.
var keptSkillGroups = [...]string{"You turned on", "Agent turned on", "Off"}

// group is the item's place in the list: the user's skills, the agent's,
// then the rest.
func (i KeptSkillItem) group() int {
	switch {
	case !i.On:
		return 2
	case i.ByAgent:
		return 1
	}
	return 0
}

// ActionToggleKeptSkill is sent when the user turns a skill on or off for
// the current chat.
type ActionToggleKeptSkill struct {
	ID   string
	Name string
	On   bool
}

// KeptSkills lets the user turn skills on or off for the current chat.
// Every skill starts off in a new chat.
type KeptSkills struct {
	com      *common.Common
	width    int
	items    []KeptSkillItem
	filtered []int // indexes into items that match the filter
	cursor   int   // index into filtered
	offset   int   // first visible row in filtered
	input    textinput.Model
	help     help.Model
	keyMap   struct {
		Up     key.Binding
		Down   key.Binding
		Toggle key.Binding
		Close  key.Binding
	}
}

var _ Dialog = (*KeptSkills)(nil)

// NewKeptSkills creates the "Skills for this chat" dialog.
func NewKeptSkills(com *common.Common, items []KeptSkillItem) *KeptSkills {
	t := com.Styles
	m := &KeptSkills{com: com, items: items}

	m.help = help.New()
	m.help.Styles = t.DialogHelpStyles()

	m.input = textinput.New()
	m.input.SetVirtualCursor(false)
	m.input.Placeholder = "Type to filter"
	m.input.SetStyles(t.TextInput)
	m.input.Focus()

	m.keyMap.Up = key.NewBinding(
		key.WithKeys("up"),
		key.WithHelp("↑/↓", "choose"),
	)
	m.keyMap.Down = key.NewBinding(
		key.WithKeys("down"),
		key.WithHelp("↑/↓", "choose"),
	)
	m.keyMap.Toggle = key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "on/off"),
	)
	m.keyMap.Close = CloseKey

	m.applyFilter()
	return m
}

// ID implements Dialog.
func (m *KeptSkills) ID() string {
	return KeptSkillsID
}

// Items returns the current items.
func (m *KeptSkills) Items() []KeptSkillItem {
	return m.items
}

// SetOn updates one skill's state without sending an action, so the open
// dialog follows changes made elsewhere, such as a chip's remove button.
func (m *KeptSkills) SetOn(id string, on bool) {
	m.SetState(id, on, false)
}

// SetState updates one skill's state and owner without sending an action,
// so the open dialog follows changes the agent makes.
func (m *KeptSkills) SetState(id string, on, byAgent bool) {
	for i := range m.items {
		if m.items[i].ID == id {
			m.items[i].On, m.items[i].ByAgent = on, on && byAgent
		}
	}
	m.regroup()
}

func (m *KeptSkills) applyFilter() {
	query := strings.ToLower(strings.TrimSpace(m.input.Value()))
	m.filtered = m.filtered[:0]
	for i, item := range m.items {
		if query == "" || strings.Contains(strings.ToLower(item.Name), query) {
			m.filtered = append(m.filtered, i)
		}
	}
	m.sortFiltered()
	m.cursor = 0
	m.offset = 0
}

// sortFiltered puts the filtered skills in their groups, keeping name order
// inside each.
func (m *KeptSkills) sortFiltered() {
	slices.SortFunc(m.filtered, func(a, b int) int {
		if g := m.items[a].group() - m.items[b].group(); g != 0 {
			return g
		}
		return a - b // items come sorted by name
	})
}

// regroup re-sorts after a state change, keeping the cursor on its skill.
func (m *KeptSkills) regroup() {
	at := -1
	if m.cursor >= 0 && m.cursor < len(m.filtered) {
		at = m.filtered[m.cursor]
	}
	m.sortFiltered()
	if i := slices.Index(m.filtered, at); i >= 0 {
		m.cursor = i
	}
}

// grouped reports whether the list shows its group headings: only once a
// skill is on, so a plain list stays plain.
func (m *KeptSkills) grouped() bool {
	return slices.ContainsFunc(m.items, func(i KeptSkillItem) bool { return i.On })
}

// HandleMsg implements Dialog.
func (m *KeptSkills) HandleMsg(msg tea.Msg) Action {
	msg2, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	switch {
	case key.Matches(msg2, m.keyMap.Close):
		return ActionClose{}
	case key.Matches(msg2, m.keyMap.Up):
		m.cursor = max(0, m.cursor-1)
	case key.Matches(msg2, m.keyMap.Down):
		m.cursor = max(0, min(len(m.filtered)-1, m.cursor+1))
	case key.Matches(msg2, m.keyMap.Toggle):
		if m.cursor < 0 || m.cursor >= len(m.filtered) {
			return nil
		}
		item := &m.items[m.filtered[m.cursor]]
		// Turning a skill on yourself makes it yours.
		item.On, item.ByAgent = !item.On, false
		m.regroup()
		return ActionToggleKeptSkill{ID: item.ID, Name: item.Name, On: item.On}
	default:
		prev := m.input.Value()
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		if m.input.Value() != prev {
			m.applyFilter()
		}
		return ActionCmd{cmd}
	}
	return nil
}

// Draw implements Dialog.
func (m *KeptSkills) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := m.com.Styles
	m.width = max(0, min(m.requiredWidth(), area.Dx()-t.Dialog.View.GetHorizontalBorderSize()))
	innerWidth := m.width - t.Dialog.View.GetHorizontalFrameSize()
	m.input.SetWidth(kit.DialogInputTextWidth(t, m.input, innerWidth))

	// Title, note, filter, the blank rows around the list, the "more"
	// hint and help take about ten rows; the list gets the rest.
	rows := max(1, min(keptSkillsMaxRows, area.Dy()-t.Dialog.View.GetVerticalFrameSize()-10))
	if m.grouped() {
		// Room for the group headings.
		rows = max(1, rows-len(keptSkillGroups))
	}

	rc := NewRenderContext(t, m.width)
	rc.Title = "Skills for this chat"
	rc.AddPart(t.Dialog.SecondaryText.Width(innerWidth).Render(keptSkillsNote))
	rc.AddPart(t.Dialog.InputPrompt.Render(m.input.View()))
	rc.AddPart(m.listView(innerWidth, rows))
	rc.Help = kit.RenderDialogHelp(t, &m.help, m, innerWidth)
	view := rc.Render()

	cur := InputCursor(t, m.input.Cursor())
	DrawCenterCursor(scr, area, view, cur)
	return cur
}

// requiredWidth fits the longest skill name and its on/off label, with a
// comfortable minimum.
func (m *KeptSkills) requiredWidth() int {
	widest := 56
	for _, item := range m.items {
		widest = max(widest, 2 /* dot + space */ +lipgloss.Width(item.Name)+1+3 /* off */)
	}
	return widest + 2 /* row padding */ + m.com.Styles.Dialog.View.GetHorizontalFrameSize()
}

// scrollTo keeps the cursor inside the visible window of rows.
func (m *KeptSkills) scrollTo(rows int) {
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+rows {
		m.offset = m.cursor - rows + 1
	}
	m.offset = max(0, min(m.offset, len(m.filtered)-rows))
}

func (m *KeptSkills) listView(innerWidth, rows int) string {
	t := m.com.Styles
	if len(m.items) == 0 {
		return t.Dialog.SecondaryText.Width(innerWidth).Render("No skills found.")
	}
	if len(m.filtered) == 0 {
		return t.Dialog.SecondaryText.Width(innerWidth).Render("No skills match.")
	}

	// The row style adds Padding(0, 1), so the text area is two columns
	// narrower than the dialog's inner width.
	rowWidth := max(0, innerWidth-2)
	m.scrollTo(rows)
	end := min(len(m.filtered), m.offset+rows)
	lines := make([]string, 0, rows+1)
	grouped := m.grouped()
	lastGroup := -1
	for pos := m.offset; pos < end; pos++ {
		item := m.items[m.filtered[pos]]
		if g := item.group(); grouped && g != lastGroup {
			lastGroup = g
			lines = append(lines, t.Dialog.SecondaryText.Render(keptSkillGroups[g]))
		}
		state := "off"
		if item.On {
			state = "on"
		}
		gap := max(1, rowWidth-2-lipgloss.Width(item.Name)-lipgloss.Width(state))

		if pos == m.cursor {
			// Plain text through the selection style: styled pieces would
			// reset the selection background partway along the row.
			lines = append(lines, t.Dialog.SelectedItem.Render(
				"● "+item.Name+strings.Repeat(" ", gap)+state,
			))
			continue
		}

		dot, stateStyle := t.Resource.DisabledIcon, t.Dialog.SecondaryText.UnsetPadding()
		if item.On {
			dot, stateStyle = t.Attachments.KeptDot, t.Attachments.KeptText
		}
		row := dot.Render() + " " +
			t.Dialog.NormalItem.UnsetPadding().Render(item.Name) +
			strings.Repeat(" ", gap) +
			stateStyle.Render(state)
		lines = append(lines, lipgloss.NewStyle().Padding(0, 1).Render(row))
	}
	if hidden := len(m.filtered) - end; hidden > 0 {
		lines = append(lines, t.Dialog.SecondaryText.Render("↓ "+strconv.Itoa(hidden)+" more"))
	} else {
		lines = append(lines, "")
	}
	return lipgloss.JoinVertical(lipgloss.Left, "", strings.Join(lines, "\n"))
}

// FullHelp implements help.KeyMap.
func (m *KeptSkills) FullHelp() [][]key.Binding {
	return [][]key.Binding{m.ShortHelp()}
}

// ShortHelp implements help.KeyMap.
func (m *KeptSkills) ShortHelp() []key.Binding {
	return []key.Binding{m.keyMap.Up, m.keyMap.Toggle, m.keyMap.Close}
}

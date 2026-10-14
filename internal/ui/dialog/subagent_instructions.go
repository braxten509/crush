package dialog

import (
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/dialog/internal/kit"
	uv "github.com/charmbracelet/ultraviolet"
)

// SubAgentInstructionsID identifies the sub-agent instructions editor.
const SubAgentInstructionsID = "subagent_instructions"

const subAgentInstructionsHint = "Every chat's agent follows these when it starts sub-agents."

// SubAgentInstructions edits the user's saved rules for starting
// sub-agents.
type SubAgentInstructions struct {
	com   *common.Common
	input textarea.Model
	help  help.Model
	save  key.Binding
}

var _ Dialog = (*SubAgentInstructions)(nil)

// NewSubAgentInstructions opens the editor on the saved text.
func NewSubAgentInstructions(com *common.Common, text string) *SubAgentInstructions {
	d := &SubAgentInstructions{com: com}
	d.input = textarea.New()
	d.input.SetStyles(com.Styles.Editor.Textarea)
	d.input.SetVirtualCursor(false)
	d.input.ShowLineNumbers = false
	d.input.CharLimit = 0
	d.input.Placeholder = "Which models to use for which work, when to ask first…"
	d.input.SetPromptFunc(0, func(textarea.PromptInfo) string { return "" })
	d.input.SetValue(text)
	d.input.Focus()
	d.help = help.New()
	d.help.Styles = com.Styles.DialogHelpStyles()
	d.save = key.NewBinding(key.WithKeys("ctrl+s"), key.WithHelp("ctrl+s", "save"))
	return d
}

// ID implements Dialog.
func (d *SubAgentInstructions) ID() string { return SubAgentInstructionsID }

// HandleMsg implements Dialog.
func (d *SubAgentInstructions) HandleMsg(msg tea.Msg) Action {
	if press, ok := msg.(tea.KeyPressMsg); ok {
		switch {
		case key.Matches(press, CloseKey):
			return ActionClose{}
		case key.Matches(press, d.save):
			return ActionSaveSubAgentInstructions{Text: d.input.Value()}
		}
	}
	var cmd tea.Cmd
	d.input, cmd = d.input.Update(msg)
	return ActionCmd{Cmd: cmd}
}

// Draw implements Dialog.
func (d *SubAgentInstructions) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := d.com.Styles
	width := max(0, min(76, area.Dx()))
	inner := max(0, width-t.Dialog.View.GetHorizontalFrameSize())
	title := t.Dialog.Title.Render(common.DialogTitle(t, t.Dialog.TitleText.Render("Sub-agent Instructions"),
		max(0, inner-t.Dialog.Title.GetHorizontalFrameSize()), t.Dialog.TitleGradFromColor, t.Dialog.TitleGradToColor))
	hint := t.Dialog.SecondaryText.Width(inner).Render(subAgentInstructionsHint)
	footer := kit.RenderDialogHelp(t, &d.help, d, inner)

	box := t.Dialog.InputPrompt
	fixed := t.Dialog.View.GetVerticalFrameSize() + lipgloss.Height(title) + lipgloss.Height(hint) +
		box.GetVerticalFrameSize() + lipgloss.Height(footer)
	d.input.SetWidth(max(1, inner-box.GetHorizontalFrameSize()))
	d.input.SetHeight(max(3, min(14, area.Dy()-fixed)))

	view := t.Dialog.View.Width(width).MaxHeight(area.Dy()).Render(strings.Join([]string{title, hint, box.Render(d.input.View()), footer}, "\n"))
	cur := d.input.Cursor()
	if cur != nil {
		cur.X += t.Dialog.View.GetBorderLeftSize() + t.Dialog.View.GetPaddingLeft() + box.GetMarginLeft() + box.GetPaddingLeft()
		cur.Y += t.Dialog.View.GetBorderTopSize() + t.Dialog.View.GetPaddingTop() + lipgloss.Height(title) + lipgloss.Height(hint) +
			box.GetMarginTop() + box.GetPaddingTop()
	}
	DrawCenterCursor(scr, area, view, cur)
	return cur
}

// ShortHelp implements help.KeyMap.
func (d *SubAgentInstructions) ShortHelp() []key.Binding { return []key.Binding{d.save, CloseKey} }

// FullHelp implements help.KeyMap.
func (d *SubAgentInstructions) FullHelp() [][]key.Binding { return [][]key.Binding{d.ShortHelp()} }

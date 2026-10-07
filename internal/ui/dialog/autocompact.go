package dialog

import (
	"errors"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/ui/common"
	uv "github.com/charmbracelet/ultraviolet"
)

// AutocompactID identifies the compaction threshold dialog.
const AutocompactID = "autocompact"

// Autocompact edits Crush's global saved token threshold.
type Autocompact struct {
	com       *common.Common
	input     textinput.Model
	help      help.Model
	submit    key.Binding
	errorText string
}

var _ Dialog = (*Autocompact)(nil)

// NewAutocompact creates the threshold editor using the saved setting.
func NewAutocompact(com *common.Common) (*Autocompact, error) {
	cfg := com.Config()
	if cfg == nil {
		return nil, errors.New("configuration not found")
	}
	d := &Autocompact{com: com}
	d.input = textinput.New()
	d.input.SetVirtualCursor(false)
	d.input.SetStyles(com.Styles.TextInput)
	d.input.Prompt = "Tokens: "
	d.input.SetValue(strconv.FormatInt(cfg.Options.GetAutoCompactTokenLimit(), 10))
	d.input.Focus()
	d.help = help.New()
	d.help.Styles = com.Styles.DialogHelpStyles()
	d.submit = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "save"))
	return d, nil
}

// ID implements Dialog.
func (d *Autocompact) ID() string { return AutocompactID }

// HandleMsg implements Dialog.
func (d *Autocompact) HandleMsg(msg tea.Msg) Action {
	if press, ok := msg.(tea.KeyPressMsg); ok {
		switch {
		case key.Matches(press, CloseKey):
			return ActionClose{}
		case key.Matches(press, d.submit):
			value := strings.ReplaceAll(strings.TrimSpace(d.input.Value()), ",", "")
			tokens, err := strconv.ParseInt(value, 10, 64)
			if err != nil {
				d.errorText = "Enter a whole number of tokens"
				return nil
			}
			if err := config.ValidateAutoCompactTokenLimit(tokens); err != nil {
				d.errorText = err.Error()
				return nil
			}
			return ActionSetAutocompact{Tokens: tokens}
		}
	}
	var cmd tea.Cmd
	d.input, cmd = d.input.Update(msg)
	d.errorText = ""
	return ActionCmd{Cmd: cmd}
}

// Draw implements Dialog.
func (d *Autocompact) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := d.com.Styles
	width := max(0, min(52, area.Dx()))
	inner := max(0, width-t.Dialog.View.GetHorizontalFrameSize())
	d.input.SetWidth(dialogInputTextWidth(t, d.input, inner))
	title := common.DialogTitle(t, t.Dialog.TitleText.Render("Fallback compaction"), max(0, inner-t.Dialog.Title.GetHorizontalFrameSize()), t.Dialog.TitleGradFromColor, t.Dialog.TitleGradToColor)
	parts := []string{t.Dialog.Title.Render(title), t.Dialog.InputPrompt.Render(d.input.View())}
	if d.errorText != "" {
		parts = append(parts, t.Dialog.TitleError.Width(inner).Render(d.errorText))
	}
	parts = append(parts, renderDialogHelp(t, &d.help, d, inner))
	view := t.Dialog.View.Width(width).MaxHeight(area.Dy()).Render(strings.Join(parts, "\n"))
	cur := InputCursor(t, d.input.Cursor())
	DrawCenterCursor(scr, area, view, cur)
	return cur
}

// ShortHelp implements help.KeyMap.
func (d *Autocompact) ShortHelp() []key.Binding { return []key.Binding{d.submit, CloseKey} }

// FullHelp implements help.KeyMap.
func (d *Autocompact) FullHelp() [][]key.Binding { return [][]key.Binding{d.ShortHelp()} }

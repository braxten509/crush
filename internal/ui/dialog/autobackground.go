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
	"github.com/charmbracelet/crush/internal/ui/dialog/internal/kit"
	uv "github.com/charmbracelet/ultraviolet"
)

// AutoBackgroundID identifies the dialog for the time before a command is
// moved to the background.
const AutoBackgroundID = "auto_background"

// AutoBackground edits the saved time, in whole seconds, a command an
// agent runs may hold the turn before Crush moves it to the background.
type AutoBackground struct {
	com       *common.Common
	input     textinput.Model
	help      help.Model
	submit    key.Binding
	errorText string
}

var _ Dialog = (*AutoBackground)(nil)

// NewAutoBackground creates the editor using the saved setting.
func NewAutoBackground(com *common.Common) (*AutoBackground, error) {
	cfg := com.Config()
	if cfg == nil {
		return nil, errors.New("configuration not found")
	}
	d := &AutoBackground{com: com}
	d.input = textinput.New()
	d.input.SetVirtualCursor(false)
	d.input.SetStyles(com.Styles.TextInput)
	d.input.Prompt = "Seconds: "
	d.input.SetValue(strconv.Itoa(int(cfg.Options.GetAutoBackground().Seconds())))
	d.input.Focus()
	d.help = help.New()
	d.help.Styles = com.Styles.DialogHelpStyles()
	d.submit = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "save"))
	return d, nil
}

// ID implements Dialog.
func (d *AutoBackground) ID() string { return AutoBackgroundID }

// HandleMsg implements Dialog.
func (d *AutoBackground) HandleMsg(msg tea.Msg) Action {
	if press, ok := msg.(tea.KeyPressMsg); ok {
		switch {
		case key.Matches(press, CloseKey):
			return ActionClose{}
		case key.Matches(press, d.submit):
			seconds, err := strconv.Atoi(strings.TrimSpace(d.input.Value()))
			if err != nil {
				d.errorText = "Enter a whole number of seconds"
				return nil
			}
			if err := config.ValidateAutoBackgroundSeconds(seconds); err != nil {
				d.errorText = err.Error()
				return nil
			}
			return ActionSetAutoBackground{Seconds: seconds}
		}
	}
	var cmd tea.Cmd
	d.input, cmd = d.input.Update(msg)
	d.errorText = ""
	return ActionCmd{Cmd: cmd}
}

// Draw implements Dialog.
func (d *AutoBackground) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := d.com.Styles
	width := max(0, min(52, area.Dx()))
	inner := max(0, width-t.Dialog.View.GetHorizontalFrameSize())
	d.input.SetWidth(kit.DialogInputTextWidth(t, d.input, inner))
	title := common.DialogTitle(t, t.Dialog.TitleText.Render("Move commands to the background after"), max(0, inner-t.Dialog.Title.GetHorizontalFrameSize()), t.Dialog.TitleGradFromColor, t.Dialog.TitleGradToColor)
	parts := []string{t.Dialog.Title.Render(title), t.Dialog.InputPrompt.Render(d.input.View())}
	if d.errorText != "" {
		parts = append(parts, t.Dialog.TitleError.Width(inner).Render(d.errorText))
	}
	parts = append(parts, kit.RenderDialogHelp(t, &d.help, d, inner))
	view := t.Dialog.View.Width(width).MaxHeight(area.Dy()).Render(strings.Join(parts, "\n"))
	cur := InputCursor(t, d.input.Cursor())
	DrawCenterCursor(scr, area, view, cur)
	return cur
}

// ShortHelp implements help.KeyMap.
func (d *AutoBackground) ShortHelp() []key.Binding { return []key.Binding{d.submit, CloseKey} }

// FullHelp implements help.KeyMap.
func (d *AutoBackground) FullHelp() [][]key.Binding { return [][]key.Binding{d.ShortHelp()} }

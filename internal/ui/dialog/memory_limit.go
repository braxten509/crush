package dialog

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/memlimit"
	"github.com/charmbracelet/crush/internal/memwatch"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/dialog/internal/kit"
	uv "github.com/charmbracelet/ultraviolet"
)

// MemoryLimitID identifies the memory limit dialog.
const MemoryLimitID = "memory_limit"

const memoryLimitHint = "Most memory all Crush windows and everything they start may use. At the limit, the biggest program inside is stopped."

// MemoryLimit edits the memory limit shared by every Crush window.
type MemoryLimit struct {
	com       *common.Common
	input     textinput.Model
	help      help.Model
	submit    key.Binding
	usage     string
	errorText string
}

var _ Dialog = (*MemoryLimit)(nil)

// NewMemoryLimit creates the limit editor showing the saved limit.
func NewMemoryLimit(com *common.Common) (*MemoryLimit, error) {
	cfg := com.Config()
	if cfg == nil {
		return nil, errors.New("configuration not found")
	}
	ram := memwatch.TotalRAM()
	d := &MemoryLimit{com: com}
	d.input = textinput.New()
	d.input.SetVirtualCursor(false)
	d.input.SetStyles(com.Styles.TextInput)
	d.input.Prompt = "GB: "
	if limit := cfg.Options.MemoryLimitBytes(ram); limit > 0 {
		d.input.SetValue(formatGB(limit))
	}
	d.input.Focus()
	d.help = help.New()
	d.help.Styles = com.Styles.DialogHelpStyles()
	d.submit = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "save"))
	d.usage = memoryUsageLine(ram)
	return d, nil
}

func memoryUsageLine(ram uint64) string {
	line := "Empty = no limit"
	if used, ok := memlimit.Usage(memlimit.Slice); ok {
		line += " · now " + formatGB(used) + " GB"
		if ram > 0 {
			line += " of " + formatGB(ram) + " GB"
		}
	}
	return line
}

// formatGB shows bytes as GB with at most one decimal: "16", "9.4".
func formatGB(b uint64) string {
	return strings.TrimSuffix(strconv.FormatFloat(float64(b)/(1<<30), 'f', 1, 64), ".0")
}

// parseMemoryLimit reads the typed limit; empty is 0, no limit.
func parseMemoryLimit(text string) (float64, error) {
	value := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(strings.ToUpper(text)), "GB"))
	if value == "" {
		return 0, nil
	}
	gb, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, fmt.Errorf("Enter a number of GB, or leave it empty for no limit")
	}
	if err := config.ValidateMemoryLimitGB(gb); err != nil {
		return 0, err
	}
	return gb, nil
}

// ID implements Dialog.
func (d *MemoryLimit) ID() string { return MemoryLimitID }

// HandleMsg implements Dialog.
func (d *MemoryLimit) HandleMsg(msg tea.Msg) Action {
	if press, ok := msg.(tea.KeyPressMsg); ok {
		switch {
		case key.Matches(press, CloseKey):
			return ActionClose{}
		case key.Matches(press, d.submit):
			gb, err := parseMemoryLimit(d.input.Value())
			if err != nil {
				d.errorText = err.Error()
				return nil
			}
			return ActionSetMemoryLimit{GB: gb}
		}
	}
	var cmd tea.Cmd
	d.input, cmd = d.input.Update(msg)
	d.errorText = ""
	return ActionCmd{Cmd: cmd}
}

// Draw implements Dialog.
func (d *MemoryLimit) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := d.com.Styles
	width := max(0, min(60, area.Dx()))
	inner := max(0, width-t.Dialog.View.GetHorizontalFrameSize())
	d.input.SetWidth(kit.DialogInputTextWidth(t, d.input, inner))
	title := common.DialogTitle(t, t.Dialog.TitleText.Render("Memory Limit"), max(0, inner-t.Dialog.Title.GetHorizontalFrameSize()), t.Dialog.TitleGradFromColor, t.Dialog.TitleGradToColor)
	hint := t.Dialog.SecondaryText.Width(inner).Render(memoryLimitHint)
	parts := []string{t.Dialog.Title.Render(title), hint, t.Dialog.InputPrompt.Render(d.input.View())}
	if d.errorText != "" {
		parts = append(parts, t.Dialog.TitleError.Width(inner).Render(d.errorText))
	}
	parts = append(parts, t.Dialog.SecondaryText.Width(inner).Render(d.usage), kit.RenderDialogHelp(t, &d.help, d, inner))
	view := t.Dialog.View.Width(width).MaxHeight(area.Dy()).Render(strings.Join(parts, "\n"))
	cur := InputCursor(t, d.input.Cursor())
	if cur != nil {
		cur.Y += lipgloss.Height(hint)
	}
	DrawCenterCursor(scr, area, view, cur)
	return cur
}

// ShortHelp implements help.KeyMap.
func (d *MemoryLimit) ShortHelp() []key.Binding { return []key.Binding{d.submit, CloseKey} }

// FullHelp implements help.KeyMap.
func (d *MemoryLimit) FullHelp() [][]key.Binding { return [][]key.Binding{d.ShortHelp()} }

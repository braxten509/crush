package dialog

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/skills"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/list"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

const SkillsID = "skills"

type SkillsResultMsg struct {
	Dialog    *Skills
	Operation string
	Query     string
	Entries   []skills.InstalledSkill
	Results   []skills.DirectorySkill
	Mutated   bool
	Err       error
}

type ActionSkillsChanged struct{}

// Skills shares Crush's dialog/list components and keeps both tabs in place
// while a request runs. Enter searches a changed query, then installs a result.
type Skills struct {
	com       *common.Common
	manager   skills.Management
	directory *skills.Directory
	input     textinput.Model
	list      *list.FilterableList
	help      help.Model
	installed []skills.InstalledSkill
	results   []skills.DirectorySkill
	browse    bool
	busy      bool
	query     string
	filter    string
	searched  string
	status    string
	cancel    context.CancelFunc
}

var _ Dialog = (*Skills)(nil)

func NewSkills(com *common.Common, manager skills.Management) *Skills {
	d := &Skills{com: com, manager: manager, directory: skills.NewDirectory()}
	d.help = help.New()
	d.help.Styles = com.Styles.DialogHelpStyles()
	d.list = list.NewFilterableList()
	d.list.Focus()
	d.input = textinput.New()
	d.input.SetVirtualCursor(false)
	d.input.SetStyles(com.Styles.TextInput)
	d.input.Placeholder = "Filter installed skills"
	d.input.Focus()
	return d
}

func (d *Skills) ID() string { return SkillsID }

func (d *Skills) Load() tea.Cmd { return d.request("refresh", "", nil) }

func (d *Skills) request(operation, query string, work func(context.Context) error) tea.Cmd {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	d.cancel = cancel
	d.busy = true
	d.status = map[string]string{"refresh": "Loading installed skills…", "search": "Searching skills.sh…", "toggle": "Saving skill setting…", "install": "Installing skill…"}[operation]
	return func() tea.Msg {
		defer cancel()
		msg := SkillsResultMsg{Dialog: d, Operation: operation, Query: query}
		if operation == "search" {
			msg.Results, msg.Err = d.directory.Search(ctx, query)
		} else {
			if work != nil {
				msg.Err = work(ctx)
				msg.Mutated = msg.Err == nil
			}
			if msg.Err == nil {
				msg.Entries, msg.Err = d.manager.ListInstalledSkills(ctx)
			}
		}
		return msg
	}
}

func (d *Skills) HandleMsg(msg tea.Msg) Action {
	switch msg := msg.(type) {
	case SkillsResultMsg:
		if msg.Dialog != d {
			return nil
		}
		d.busy = false
		d.cancel = nil
		if msg.Err != nil {
			d.status = "Error: " + msg.Err.Error()
			if msg.Mutated {
				return ActionSkillsChanged{}
			}
			return nil
		}
		if msg.Operation == "search" {
			d.results, d.searched = msg.Results, msg.Query
			d.status = fmt.Sprintf("%d results from skills.sh", len(d.results))
		} else {
			d.installed = msg.Entries
			d.status = "Settings apply to subsequent turns in Crush."
			if msg.Operation == "install" {
				d.status = "Installed in the shared skills folder."
			}
		}
		d.setItems()
		if msg.Operation == "install" || msg.Operation == "toggle" {
			return ActionSkillsChanged{}
		}
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc", "alt+esc":
			if d.cancel != nil {
				d.cancel()
			}
			return ActionClose{}
		case "tab", "shift+tab":
			if d.browse {
				d.query = d.input.Value()
			} else {
				d.filter = d.input.Value()
			}
			d.browse = !d.browse
			if d.browse {
				d.input.Placeholder = "Search skills.sh (Enter to search)"
				d.input.SetValue(d.query)
			} else {
				d.input.Placeholder = "Filter installed skills"
				d.input.SetValue(d.filter)
			}
			d.setItems()
		case "down", "ctrl+n":
			d.list.SelectNext()
			d.list.ScrollToSelected()
		case "up", "ctrl+p":
			d.list.SelectPrev()
			d.list.ScrollToSelected()
		case "ctrl+r":
			if !d.busy {
				return ActionCmd{d.Load()}
			}
		case "enter":
			if d.busy {
				return nil
			}
			if d.browse && (strings.TrimSpace(d.input.Value()) != d.searched || d.searched == "") {
				query := strings.TrimSpace(d.input.Value())
				if len([]rune(query)) < 2 {
					d.status = "Type at least two characters to search skills.sh."
					return nil
				}
				return ActionCmd{d.request("search", query, nil)}
			}
			item, ok := d.list.SelectedItem().(*CommandItem)
			if !ok {
				return nil
			}
			if d.browse {
				index := slices.IndexFunc(d.results, func(s skills.DirectorySkill) bool { return s.ID == item.ID() })
				if index < 0 {
					return nil
				}
				selected := d.results[index]
				if d.isInstalled(selected) {
					d.status = "Already installed. Switch to Installed to enable or disable it."
					return nil
				}
				return ActionCmd{d.request("install", "", func(ctx context.Context) error { return d.manager.InstallSkill(ctx, selected) })}
			}
			index := slices.IndexFunc(d.installed, func(s skills.InstalledSkill) bool { return s.ID == item.ID() })
			if index < 0 {
				return nil
			}
			selected := d.installed[index]
			return ActionCmd{d.request("toggle", "", func(ctx context.Context) error {
				return d.manager.SetSkillEnabled(ctx, selected.Name, !selected.Enabled)
			})}
		default:
			old := d.input.Value()
			var cmd tea.Cmd
			d.input, cmd = d.input.Update(msg)
			if d.input.Value() != old {
				if !d.browse {
					d.list.SetFilter(d.input.Value())
					d.list.SetSelected(0)
					d.list.ScrollToTop()
				}
			}
			return ActionCmd{cmd}
		}
	}
	return nil
}

func (d *Skills) isInstalled(s skills.DirectorySkill) bool {
	name := s.SkillID
	if name == "" {
		parts := strings.Split(s.ID, "/")
		name = parts[len(parts)-1]
	}
	return slices.ContainsFunc(d.installed, func(i skills.InstalledSkill) bool { return i.Name == name })
}

func (d *Skills) setItems() {
	selected := ""
	if item, ok := d.list.SelectedItem().(*CommandItem); ok {
		selected = item.ID()
	}
	var items []list.FilterableItem
	if d.browse {
		for _, s := range d.results {
			status := fmt.Sprintf("%d installs", s.Installs)
			if d.isInstalled(s) {
				status = "Installed"
			}
			items = append(items, NewCommandItem(d.com.Styles, s.ID, plainSkillText(s.Name), status, nil).WithAliases(s.Source))
		}
	} else {
		for _, s := range d.installed {
			status := "Disabled"
			if s.Enabled {
				status = "Enabled"
			}
			items = append(items, NewCommandItem(d.com.Styles, s.ID, plainSkillText(s.Name), status, nil).WithAliases(s.Description).WithMuted(!s.Enabled))
		}
	}
	d.list.SetItems(items...)
	filter := ""
	if !d.browse {
		filter = d.input.Value()
	}
	d.list.SetFilter(filter)
	index := slices.IndexFunc(d.list.FilteredItems(), func(i list.Item) bool { return i.(*CommandItem).ID() == selected })
	d.list.SetSelected(max(0, index))
	d.list.ScrollToSelected()
}

// Network metadata and file descriptions are plain text in the terminal.
func plainSkillText(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 32 || r == 127 || r >= 0x80 && r <= 0x9f {
			return ' '
		}
		return r
	}, s)
}

func (d *Skills) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := d.com.Styles
	width := max(0, min(80, area.Dx()-t.Dialog.View.GetHorizontalBorderSize()))
	height := max(0, min(22, area.Dy()-t.Dialog.View.GetVerticalBorderSize()))
	innerWidth := max(0, width-t.Dialog.View.GetHorizontalFrameSize())
	offset := t.Dialog.Title.GetVerticalFrameSize() + titleContentHeight + t.Dialog.InputPrompt.GetVerticalFrameSize() + inputContentHeight + t.Dialog.HelpView.GetVerticalFrameSize() + t.Dialog.View.GetVerticalFrameSize() + 3
	d.input.SetWidth(dialogInputTextWidth(t, d.input, innerWidth))
	d.list.SetSize(innerWidth, max(0, height-offset))
	rc := NewRenderContext(t, width)
	rc.Title = "Skills · Installed"
	if d.browse {
		rc.Title = "Skills · Browse skills.sh"
	}
	rc.AddPart(t.Dialog.InputPrompt.Render(d.input.View()))
	view := d.list.Render()
	if len(d.list.FilteredItems()) == 0 {
		view = "No installed skills match this filter."
		if d.browse {
			view = "Search skills.sh above, then select a skill to install."
			if d.searched != "" {
				view = "No results. Try another search."
			}
		}
		view = t.Dialog.SecondaryText.Render(ansi.Truncate(view, innerWidth, "…"))
	}
	rc.AddPart(t.Dialog.List.Height(d.list.Height()).Render(view))
	detail := ""
	if item, ok := d.list.SelectedItem().(*CommandItem); ok {
		if d.browse {
			for _, s := range d.results {
				if s.ID == item.ID() {
					detail = s.Source + " · https://skills.sh/" + s.ID
					break
				}
			}
		} else {
			for _, s := range d.installed {
				if s.ID == item.ID() {
					detail = s.Description
					break
				}
			}
		}
	}
	textWidth := max(0, innerWidth-t.Dialog.SecondaryText.GetHorizontalFrameSize())
	rc.AddPart(t.Dialog.SecondaryText.Width(innerWidth).Height(1).Render(ansi.Truncate(plainSkillText(detail), textWidth, "…")))
	rc.AddPart(t.Dialog.SecondaryText.Width(innerWidth).Height(1).Render(ansi.Truncate(plainSkillText(d.status), textWidth, "…")))
	rc.Help = renderDialogHelp(t, &d.help, d, innerWidth)
	cur := InputCursor(t, d.input.Cursor())
	DrawCenterCursor(scr, area, rc.Render(), cur)
	return cur
}

func (d *Skills) ShortHelp() []key.Binding {
	enter := "toggle"
	tab := "browse"
	if d.browse {
		tab = "installed"
		enter = "search"
		if d.searched != "" && strings.TrimSpace(d.input.Value()) == d.searched {
			enter = "install"
		}
	}
	return []key.Binding{key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", enter)), key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", tab)), CloseKey, key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑/↓", "choose")), key.NewBinding(key.WithKeys("ctrl+r"), key.WithHelp("ctrl+r", "refresh"))}
}
func (d *Skills) FullHelp() [][]key.Binding { return [][]key.Binding{d.ShortHelp()} }

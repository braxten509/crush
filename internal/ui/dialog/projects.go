package dialog

import (
	"errors"
	"slices"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/home"
	"github.com/charmbracelet/crush/internal/projects"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/list"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/charmbracelet/crush/internal/ui/util"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/sahilm/fuzzy"
)

const (
	// ProjectsID is the identifier for the saved projects dialog.
	ProjectsID              = "projects"
	projectsDialogMaxWidth  = 80
	projectsDialogMinHeight = 8
	projectsDialogMaxHeight = 20
)

// Projects is a dialog for opening a saved project.
type Projects struct {
	com   *common.Common
	help  help.Model
	list  *list.FilterableList
	items []list.FilterableItem
	input textinput.Model

	keyMap struct {
		Select   key.Binding
		Remove   key.Binding
		Next     key.Binding
		Previous key.Binding
		UpDown   key.Binding
		Close    key.Binding
	}
}

// ProjectItem is a saved project list item.
type ProjectItem struct {
	*list.Versioned
	path      string
	title     string
	isCurrent bool
	t         *styles.Styles
	m         fuzzy.Match
	cache     map[int]string
	focused   bool
}

var (
	_ Dialog   = (*Projects)(nil)
	_ ListItem = (*ProjectItem)(nil)
)

// NewProjects creates the saved projects dialog.
func NewProjects(com *common.Common) (*Projects, error) {
	saved, err := projects.SavedList()
	if err != nil {
		return nil, err
	}
	if len(saved) == 0 {
		return nil, errors.New("no saved projects yet, use Save Project first")
	}

	p := &Projects{com: com}
	p.help = help.New()
	p.help.Styles = com.Styles.DialogHelpStyles()

	p.list = list.NewFilterableList()
	p.list.Focus()

	p.input = textinput.New()
	p.input.SetVirtualCursor(false)
	p.input.Placeholder = "Type to filter"
	p.input.SetStyles(com.Styles.TextInput)
	p.input.Focus()

	p.keyMap.Select = key.NewBinding(key.WithKeys("enter", "ctrl+y"), key.WithHelp("enter", "open"))
	p.keyMap.Remove = key.NewBinding(key.WithKeys("ctrl+x"), key.WithHelp("ctrl+x", "remove"))
	p.keyMap.Next = key.NewBinding(key.WithKeys("down", "ctrl+n"), key.WithHelp("↓", "next item"))
	p.keyMap.Previous = key.NewBinding(key.WithKeys("up", "ctrl+p"), key.WithHelp("↑", "previous item"))
	p.keyMap.UpDown = key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑/↓", "choose"))
	p.keyMap.Close = CloseKey

	cwd := com.Workspace.WorkingDir()
	for _, sp := range saved {
		p.items = append(p.items, &ProjectItem{
			Versioned: list.NewVersioned(),
			path:      sp.Path,
			title:     home.Short(sp.Path),
			isCurrent: sp.Path == cwd,
			t:         com.Styles,
		})
	}
	p.list.SetItems(p.items...)
	p.list.SetSelected(0)
	return p, nil
}

// ID implements Dialog.
func (p *Projects) ID() string { return ProjectsID }

// HandleMsg implements [Dialog].
func (p *Projects) HandleMsg(msg tea.Msg) Action {
	km, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	switch {
	case key.Matches(km, p.keyMap.Close):
		return ActionClose{}
	case key.Matches(km, p.keyMap.Previous):
		if p.list.IsSelectedFirst() {
			p.list.SelectLast()
		} else {
			p.list.SelectPrev()
		}
		p.list.ScrollToSelected()
	case key.Matches(km, p.keyMap.Next):
		if p.list.IsSelectedLast() {
			p.list.SelectFirst()
		} else {
			p.list.SelectNext()
		}
		p.list.ScrollToSelected()
	case key.Matches(km, p.keyMap.Remove):
		item, ok := p.list.SelectedItem().(*ProjectItem)
		if !ok {
			break
		}
		sel := p.list.Selected()
		p.items = slices.DeleteFunc(p.items, func(i list.FilterableItem) bool { return i == item })
		p.list.SetItems(p.items...)
		p.list.SetFilter(p.input.Value())
		p.list.SetSelected(min(sel, len(p.list.FilteredItems())-1))
		return ActionCmd{func() tea.Msg {
			if err := projects.Unsave(item.path); err != nil {
				return util.ReportError(err)()
			}
			return util.NewInfoMsg("Removed saved project " + item.title)
		}}
	case key.Matches(km, p.keyMap.Select):
		if item, ok := p.list.SelectedItem().(*ProjectItem); ok {
			return ActionOpenProject{Path: item.path}
		}
	default:
		prev := p.input.Value()
		var cmd tea.Cmd
		p.input, cmd = p.input.Update(msg)
		if p.input.Value() != prev {
			p.list.SetFilter(p.input.Value())
			p.list.ScrollToTop()
			p.list.SetSelected(0)
		}
		return ActionCmd{cmd}
	}
	return nil
}

// Cursor returns the cursor position relative to the dialog.
func (p *Projects) Cursor() *tea.Cursor {
	return InputCursor(p.com.Styles, p.input.Cursor())
}

// Draw implements [Dialog].
func (p *Projects) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := p.com.Styles
	width := max(0, min(projectsDialogMaxWidth, area.Dx()-t.Dialog.View.GetHorizontalBorderSize()))
	innerWidth := width - t.Dialog.View.GetHorizontalFrameSize()
	p.input.SetWidth(dialogInputTextWidth(t, p.input, innerWidth))

	heightOffset := t.Dialog.Title.GetVerticalFrameSize() + titleContentHeight +
		t.Dialog.InputPrompt.GetVerticalFrameSize() + inputContentHeight +
		t.Dialog.HelpView.GetVerticalFrameSize() +
		t.Dialog.View.GetVerticalFrameSize()
	maxAvailable := area.Dy() - t.Dialog.View.GetVerticalBorderSize()
	height := max(projectsDialogMinHeight, min(projectsDialogMaxHeight, heightOffset+p.list.TotalHeight(), maxAvailable))

	listHeight, listTotalHeight, _ := sizeDialogList(t, p.list, innerWidth, height)

	rc := NewRenderContext(t, width)
	rc.Title = "Open Project"
	rc.AddPart(t.Dialog.InputPrompt.Render(p.input.View()))

	if p.list.Height() >= len(p.list.FilteredItems()) {
		p.list.ScrollToTop()
	} else {
		p.list.ScrollToSelected()
	}
	listView := t.Dialog.List.Height(p.list.Height()).Render(p.list.Render())
	rc.AddPart(joinScrollbar(t, listView, listHeight, listTotalHeight, listHeight, p.list.Offset()))
	rc.Help = renderDialogHelp(t, &p.help, p, innerWidth)

	cur := p.Cursor()
	DrawCenterCursor(scr, area, rc.Render(), cur)
	return cur
}

// ShortHelp implements [help.KeyMap].
func (p *Projects) ShortHelp() []key.Binding {
	return []key.Binding{p.keyMap.UpDown, p.keyMap.Select, p.keyMap.Remove, p.keyMap.Close}
}

// FullHelp implements [help.KeyMap].
func (p *Projects) FullHelp() [][]key.Binding {
	return [][]key.Binding{{p.keyMap.Select, p.keyMap.Remove, p.keyMap.Next, p.keyMap.Previous, p.keyMap.Close}}
}

// Finished implements list.Item.
func (i *ProjectItem) Finished() bool { return true }

// Filter implements list.FilterableItem.
func (i *ProjectItem) Filter() string { return i.title }

// ID implements list.Item.
func (i *ProjectItem) ID() string { return i.path }

// SetFocused sets the focus state of the item.
func (i *ProjectItem) SetFocused(focused bool) {
	if i.focused == focused {
		return
	}
	i.cache = nil
	i.focused = focused
	i.Bump()
}

// SetMatch sets the fuzzy match for the item.
func (i *ProjectItem) SetMatch(m fuzzy.Match) {
	if sameFuzzyMatch(i.m, m) {
		return
	}
	i.cache = nil
	i.m = m
	i.Bump()
}

// Render returns the string representation of the item.
func (i *ProjectItem) Render(width int) string {
	info := ""
	if i.isCurrent {
		info = "current"
	}
	return renderItem(ListItemStyles{
		ItemBlurred:     i.t.Dialog.NormalItem,
		ItemFocused:     i.t.Dialog.SelectedItem,
		InfoTextBlurred: i.t.Dialog.ListItem.InfoBlurred,
		InfoTextFocused: i.t.Dialog.ListItem.InfoFocused,
	}, i.title, info, i.focused, width, i.cache, &i.m)
}

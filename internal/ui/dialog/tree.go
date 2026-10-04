package dialog

import (
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/list"
	uv "github.com/charmbracelet/ultraviolet"
	"strings"
)

const TreeID = "tree"

type ActionTreeJump struct{ MessageID string }
type ActionTreeLabel struct{ MessageID, Label string }

type Tree struct {
	com      *common.Common
	entries  []message.TreeEntry
	list     *list.FilterableList
	input    textinput.Model
	labeling string
}

func NewTree(com *common.Common, entries []message.TreeEntry) *Tree {
	d := &Tree{com: com, entries: entries}
	d.input = textinput.New()
	d.input.SetVirtualCursor(false)
	d.input.SetStyles(com.Styles.TextInput)
	d.input.Placeholder = "Search branches"
	d.input.Focus()
	d.list = list.NewFilterableList()
	d.list.Focus()
	d.rebuild()
	return d
}
func (d *Tree) ID() string { return TreeID }
func (d *Tree) rebuild() {
	children := map[string][]message.TreeEntry{}
	for _, e := range d.entries {
		children[e.ParentID] = append(children[e.ParentID], e)
	}
	var items []list.FilterableItem
	selected := 0
	var walk func(string, int)
	walk = func(parent string, depth int) {
		for _, e := range children[parent] {
			marker := "  "
			if e.Active {
				marker = "* "
				selected = len(items)
			}
			title := strings.Join(strings.Fields(e.Message.Content().Text), " ")
			if title == "" {
				title = "[tool activity]"
			}
			prefix := marker + strings.Repeat("  ", min(depth, 8)) + string(e.Message.Role) + ": "
			if e.Label != "" {
				prefix += "[" + e.Label + "] "
			}
			items = append(items, NewCommandItem(d.com.Styles, e.MessageID, prefix+title, "", ActionTreeJump{e.MessageID}))
			walk(e.MessageID, depth+1)
		}
	}
	walk("", 0)
	d.list.SetItems(items...)
	d.list.SetSelected(selected)
}
func (d *Tree) HandleMsg(msg tea.Msg) Action {
	if k, ok := msg.(tea.KeyPressMsg); ok {
		switch k.String() {
		case "esc":
			if d.labeling != "" {
				d.labeling = ""
				d.input.SetValue("")
				d.input.Placeholder = "Search branches"
				return nil
			}
			return ActionClose{}
		case "up", "ctrl+p":
			d.list.SelectPrev()
			d.list.ScrollToSelected()
			return nil
		case "down", "ctrl+n":
			d.list.SelectNext()
			d.list.ScrollToSelected()
			return nil
		case "enter":
			if d.labeling != "" {
				return ActionTreeLabel{d.labeling, strings.TrimSpace(d.input.Value())}
			}
			if item, ok := d.list.SelectedItem().(*CommandItem); ok {
				return item.Action()
			}
			return nil
		case "ctrl+r":
			if item, ok := d.list.SelectedItem().(*CommandItem); ok {
				d.labeling = item.ID()
				d.input.SetValue("")
				d.input.Placeholder = "Bookmark name (empty clears)"
			}
			return nil
		}
	}
	previous := d.input.Value()
	var cmd tea.Cmd
	d.input, cmd = d.input.Update(msg)
	if d.labeling == "" && d.input.Value() != previous {
		d.list.SetFilter(d.input.Value())
		d.list.SetSelected(0)
		d.list.ScrollToSelected()
	}
	return ActionCmd{cmd}
}
func (d *Tree) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := d.com.Styles
	width := max(0, min(defaultDialogMaxWidth, area.Dx()-t.Dialog.View.GetHorizontalBorderSize()))
	height := max(0, min(defaultDialogHeight, area.Dy()-t.Dialog.View.GetVerticalBorderSize()))
	inner := max(0, width-t.Dialog.View.GetHorizontalFrameSize())
	d.input.SetWidth(dialogInputTextWidth(t, d.input, inner))
	listHeight, totalHeight, _ := sizeDialogList(t, d.list, inner, max(0, height-2))
	if totalHeight <= listHeight {
		d.list.ScrollToTop()
	}
	d.list.ScrollToSelected()
	rc := NewRenderContext(t, width)
	rc.Title = "Session tree"
	rc.AddPart(t.Dialog.InputPrompt.Render(d.input.View()))
	rc.AddPart(t.Dialog.List.Height(d.list.Height()).Render(d.list.Render()))
	rc.AddPart(t.Dialog.List.Render(" Files stay as they are. Continue after selected entry."))
	rc.AddPart(t.Dialog.List.Render(" ↑↓ choose  enter jump  ctrl+r bookmark  esc close"))
	cur := InputCursor(t, d.input.Cursor())
	DrawCenterCursor(scr, area, rc.Render(), cur)
	return cur
}

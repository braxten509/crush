package dialog

import (
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"context"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/list"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"strings"
)

const TreeID = "tree"
const ForkID = "fork"
const treeRootID = "__tree_root__"

type ActionTreeJump struct{ MessageID string }
type ActionTreeLabel struct{ MessageID, Label string }
type ActionTreeEdit struct{ MessageID string }
type ActionTreeCopy struct {
	MessageID string
	Fork      bool
}
type ActionTreeClone struct{}
type ActionTreeNavigate struct {
	MessageID, Prompt string
	Summary           bool
}

type Tree struct {
	EditMessageID  string
	com            *common.Common
	entries        []message.TreeEntry
	list           *list.FilterableList
	input          textinput.Model
	labeling       string
	folded         map[string]bool
	filter         int
	Fork           bool
	confirming     bool
	summary        bool
	target, prompt string
	cancel         context.CancelFunc
}

var treeFilters = []string{"default", "no tools", "user only", "labeled only", "all"}

func NewTree(com *common.Common, entries []message.TreeEntry) *Tree {
	d := &Tree{com: com, entries: entries, folded: map[string]bool{}}
	d.input = textinput.New()
	d.input.SetVirtualCursor(false)
	d.input.SetStyles(com.Styles.TextInput)
	d.input.Placeholder = "Search text or bookmarks"
	d.input.Focus()
	d.list = list.NewFilterableList()
	d.list.Focus()
	d.rebuild()
	return d
}
func (d *Tree) ID() string { return TreeID }
func (d *Tree) selectedID() string {
	if item, ok := d.list.SelectedItem().(*CommandItem); ok {
		return item.ID()
	}
	return ""
}
func (d *Tree) Confirm(target, prompt string) {
	d.EditMessageID = ""
	d.confirming = true
	d.summary = false
	d.target = target
	d.prompt = prompt
}
func (d *Tree) Working(cancel context.CancelFunc) { d.cancel = cancel }
func (d *Tree) Finish()                           { d.cancel = nil; d.confirming = false }
func (d *Tree) SetFork()                          { d.Fork = true; d.filter = 2; d.rebuild() }
func (d *Tree) rebuild() {
	keep := d.selectedID()
	children := map[string][]message.TreeEntry{}
	for _, e := range d.entries {
		children[e.ParentID] = append(children[e.ParentID], e)
	}
	var items []list.FilterableItem
	selected := 0
	if !d.Fork {
		marker := "* "
		for _, entry := range d.entries {
			if entry.Active {
				marker = "  "
				break
			}
		}
		items = append(items, NewCommandItem(d.com.Styles, treeRootID, marker+"Start of chat (before first message)", "", ActionTreeJump{""}))
	}
	// Iterative traversal keeps very deep, linear histories off the Go stack.
	type row struct {
		entry message.TreeEntry
		depth int
	}
	var stack []row
	roots := children[""]
	for i := len(roots) - 1; i >= 0; i-- {
		stack = append(stack, row{roots[i], 0})
	}
	for len(stack) > 0 {
		current := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		e, depth := current.entry, current.depth
		title := strings.Join(strings.Fields(e.Message.Content().Text), " ")
		show := true
		switch d.filter {
		case 0:
			show = e.Message.Role != message.Tool && (title != "" || e.Label != "")
		case 1:
			show = e.Message.Role != message.Tool
		case 2:
			show = e.Message.Role == message.User
		case 3:
			show = e.Label != ""
		}
		if d.Fork {
			show = e.Message.Role == message.User
		}
		if show {
			marker := "  "
			if e.Active {
				marker = "* "
				if keep == "" {
					selected = len(items)
				}
			}
			if e.MessageID == keep {
				selected = len(items)
			}
			fold := "  "
			if len(children[e.MessageID]) > 0 {
				fold = "− "
				if d.folded[e.MessageID] {
					fold = "+ "
				}
			}
			if title == "" {
				title = "[tool activity]"
			}
			prefix := marker + strings.Repeat("  ", min(depth, 4)) + fold + string(e.Message.Role) + ": "
			if e.Label != "" {
				prefix += "[" + e.Label + "] "
			}
			items = append(items, NewCommandItem(d.com.Styles, e.MessageID, prefix+title, "", ActionTreeJump{e.MessageID}))
		}
		if !d.folded[e.MessageID] || d.input.Value() != "" {
			descendants := children[e.MessageID]
			nextDepth := depth
			if len(descendants) > 1 {
				nextDepth++
			}
			for i := len(descendants) - 1; i >= 0; i-- {
				stack = append(stack, row{descendants[i], nextDepth})
			}
		}
	}
	d.list.SetItems(items...)
	d.list.SetFilter(d.input.Value())
	d.list.SetSelected(selected)
	d.list.ScrollToSelected()
}
func (d *Tree) branch(direction int) {
	counts := map[string]int{}
	for _, e := range d.entries {
		counts[e.ParentID]++
	}
	branches := map[string]bool{}
	for _, e := range d.entries {
		if counts[e.MessageID] == 0 || counts[e.ParentID] > 1 {
			branches[e.MessageID] = true
		}
	}
	items := d.list.FilteredItems()
	if len(items) == 0 {
		return
	}
	start := d.list.Selected()
	for step := 1; step <= len(items); step++ {
		index := (start + direction*step + len(items)*2) % len(items)
		if item, ok := items[index].(*CommandItem); ok && branches[item.ID()] {
			d.list.SetSelected(index)
			d.list.ScrollToSelected()
			return
		}
	}
}
func (d *Tree) HandleMsg(msg tea.Msg) Action {
	if k, ok := msg.(tea.KeyPressMsg); ok {
		if d.cancel != nil {
			if k.String() == "esc" {
				d.cancel()
			}
			return nil
		}
		if d.confirming {
			switch k.String() {
			case "esc":
				d.confirming = false
				return nil
			case "tab", "left", "right", "up", "down":
				d.summary = !d.summary
			case "enter":
				return ActionTreeNavigate{d.target, d.prompt, d.summary}
			}
			return nil
		}
		switch k.String() {
		case "esc":
			if d.labeling != "" {
				d.labeling = ""
				d.input.SetValue("")
				d.input.Placeholder = "Search text or bookmarks"
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
				if d.Fork {
					return ActionTreeCopy{item.ID(), true}
				}
				return item.Action()
			}
			return nil
		case "ctrl+r":
			if id := d.selectedID(); id != "" && id != treeRootID {
				d.labeling = id
				d.input.SetValue("")
				d.input.Placeholder = "Bookmark name (empty clears)"
			}
			return nil
		case "ctrl+e":
			if d.labeling == "" {
				return ActionTreeEdit{d.selectedID()}
			}
		case "tab", "shift+tab":
			if d.labeling == "" && !d.Fork {
				delta := 1
				if k.String() == "shift+tab" {
					delta = -1
				}
				d.filter = (d.filter + delta + len(treeFilters)) % len(treeFilters)
				d.rebuild()
				return nil
			}
		case "left", "right":
			if d.labeling == "" {
				d.folded[d.selectedID()] = k.String() == "left"
				d.rebuild()
				return nil
			}
		case "alt+up":
			d.branch(-1)
			return nil
		case "alt+down":
			d.branch(1)
			return nil
		}
	}
	previous := d.input.Value()
	var cmd tea.Cmd
	d.input, cmd = d.input.Update(msg)
	if d.labeling == "" && d.input.Value() != previous {
		d.rebuild()
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
	listHeight, totalHeight, _ := sizeDialogList(t, d.list, inner, max(0, height-3))
	if totalHeight <= listHeight {
		d.list.ScrollToTop()
	}
	d.list.ScrollToSelected()
	rc := NewRenderContext(t, width)
	rc.Title = "Session tree · " + treeFilters[d.filter]
	if d.Fork {
		rc.Title = "Fork · choose a prompt to edit in a new chat"
	}
	rc.Title = ansi.Truncate(rc.Title, max(0, inner-2), "…")
	rc.AddPart(t.Dialog.InputPrompt.Render(d.input.View()))
	rc.AddPart(t.Dialog.List.Height(d.list.Height()).Render(d.list.Render()))
	lines := []string{"File changes are previewed before a jump.", "↑↓ choose  enter jump  ←→ fold  tab filter", "ctrl+e edit  ctrl+r label  alt+↑↓ branch  esc close"}
	if d.confirming {
		choice := "[No summary]   Add summary"
		if d.summary {
			choice = " No summary   [Add summary]"
		}
		lines = []string{"Carry a summary of the branch you are leaving?", choice, "←→ choose  enter confirm  esc back · File preview follows."}
	}
	if d.cancel != nil {
		status := "Switching branch…"
		if d.summary {
			status = "Writing branch summary…"
		}
		lines = []string{status, "Your old branch stays selected until it succeeds.", "esc cancel · Files wait until the summary succeeds."}
	}
	for i, line := range lines {
		lines[i] = ansi.Truncate(line, max(0, inner-2), "…")
	}
	rc.AddPart(t.Dialog.List.Render(strings.Join(lines, "\n")))
	cur := InputCursor(t, d.input.Cursor())
	DrawCenterCursor(scr, area, rc.Render(), cur)
	return cur
}

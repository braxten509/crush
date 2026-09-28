package dialog

import (
	"fmt"
	"slices"
	"strconv"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/agent"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/list"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/charmbracelet/crush/internal/ui/util"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/sahilm/fuzzy"
)

const (
	// BackgroundID is the identifier for the background work dialog.
	BackgroundID              = "background"
	backgroundDialogMaxWidth  = 100
	backgroundDialogMinHeight = 6
	backgroundDialogMaxHeight = 24
)

// Background lists the running sub-agents and the processes the agents
// left running, and stops or kills the selected one.
type Background struct {
	com   *common.Common
	help  help.Model
	list  *list.FilterableList
	items []list.FilterableItem

	keyMap struct {
		Stop     key.Binding
		Next     key.Binding
		Previous key.Binding
		UpDown   key.Binding
		Close    key.Binding
	}
}

// BackgroundItem is a sub-agent or a process.
type BackgroundItem struct {
	*list.Versioned
	task    *agent.Task
	proc    *agent.Process
	t       *styles.Styles
	m       fuzzy.Match
	cache   map[int]string
	focused bool
}

var (
	_ Dialog   = (*Background)(nil)
	_ ListItem = (*BackgroundItem)(nil)
)

// NewBackground creates the dialog for the given sub-agents and processes,
// selecting the item with the given ID if there is one.
func NewBackground(com *common.Common, tasks []agent.Task, procs []agent.Process, selected string) *Background {
	b := &Background{com: com}
	b.help = help.New()
	b.help.Styles = com.Styles.DialogHelpStyles()
	b.list = list.NewFilterableList()
	b.list.Focus()

	b.keyMap.Stop = key.NewBinding(key.WithKeys("x", "delete", "backspace"), key.WithHelp("x", "stop/kill"))
	b.keyMap.Next = key.NewBinding(key.WithKeys("down", "j", "ctrl+n"), key.WithHelp("↓", "next item"))
	b.keyMap.Previous = key.NewBinding(key.WithKeys("up", "k", "ctrl+p"), key.WithHelp("↑", "previous item"))
	b.keyMap.UpDown = key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑/↓", "choose"))
	b.keyMap.Close = CloseKey

	for _, t := range tasks {
		if t.Status == agent.TaskRunning {
			b.items = append(b.items, &BackgroundItem{Versioned: list.NewVersioned(), task: &t, t: com.Styles})
		}
	}
	for _, p := range procs {
		b.items = append(b.items, &BackgroundItem{Versioned: list.NewVersioned(), proc: &p, t: com.Styles})
	}
	b.list.SetItems(b.items...)
	sel := slices.IndexFunc(b.items, func(i list.FilterableItem) bool { return i.(*BackgroundItem).ID() == selected })
	b.list.SetSelected(max(sel, 0))
	return b
}

// ID implements Dialog.
func (b *Background) ID() string { return BackgroundID }

// HandleMsg implements [Dialog].
func (b *Background) HandleMsg(msg tea.Msg) Action {
	km, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	switch {
	case key.Matches(km, b.keyMap.Close):
		return ActionClose{}
	case key.Matches(km, b.keyMap.Previous):
		if b.list.IsSelectedFirst() {
			b.list.SelectLast()
		} else {
			b.list.SelectPrev()
		}
		b.list.ScrollToSelected()
	case key.Matches(km, b.keyMap.Next):
		if b.list.IsSelectedLast() {
			b.list.SelectFirst()
		} else {
			b.list.SelectNext()
		}
		b.list.ScrollToSelected()
	case key.Matches(km, b.keyMap.Stop):
		item, ok := b.list.SelectedItem().(*BackgroundItem)
		if !ok {
			break
		}
		sel := b.list.Selected()
		b.items = slices.DeleteFunc(b.items, func(i list.FilterableItem) bool { return i == item })
		b.list.SetItems(b.items...)
		b.list.SetSelected(min(sel, len(b.items)-1))
		cmd := func() tea.Msg {
			if item.task != nil {
				if err := agent.StopTask(item.task.ID); err != nil {
					return util.ReportError(err)()
				}
				return util.NewInfoMsg("Stopped sub-agent " + item.task.Name)
			}
			if err := agent.KillProcess(item.proc.PID); err != nil {
				return util.ReportError(err)()
			}
			return util.NewInfoMsg("Killed " + item.proc.Command)
		}
		return ActionCmd{cmd}
	}
	return nil
}

// Draw implements [Dialog].
func (b *Background) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := b.com.Styles
	width := max(0, min(backgroundDialogMaxWidth, area.Dx()-t.Dialog.View.GetHorizontalBorderSize()))
	innerWidth := width - t.Dialog.View.GetHorizontalFrameSize()

	// Sized like the dialogs with a filter input, as sizeDialogList
	// assumes one.
	heightOffset := t.Dialog.Title.GetVerticalFrameSize() + titleContentHeight +
		t.Dialog.InputPrompt.GetVerticalFrameSize() + inputContentHeight +
		t.Dialog.HelpView.GetVerticalFrameSize() +
		t.Dialog.View.GetVerticalFrameSize()
	maxAvailable := area.Dy() - t.Dialog.View.GetVerticalBorderSize()
	height := max(backgroundDialogMinHeight, min(backgroundDialogMaxHeight, heightOffset+len(b.items), maxAvailable))
	listHeight, listTotalHeight, _ := sizeDialogList(t, b.list, innerWidth, height)

	rc := NewRenderContext(t, width)
	rc.Title = "Background"
	if len(b.items) == 0 {
		rc.AddPart(t.Dialog.NormalItem.Render("Nothing is running in the background."))
	} else {
		b.list.ScrollToSelected()
		listView := t.Dialog.List.Height(b.list.Height()).Render(b.list.Render())
		rc.AddPart(joinScrollbar(t, listView, listHeight, listTotalHeight, listHeight, b.list.Offset()))
	}
	rc.Help = renderDialogHelp(t, &b.help, b, innerWidth)
	DrawCenterCursor(scr, area, rc.Render(), nil)
	return nil
}

// ShortHelp implements [help.KeyMap].
func (b *Background) ShortHelp() []key.Binding {
	return []key.Binding{b.keyMap.UpDown, b.keyMap.Stop, b.keyMap.Close}
}

// FullHelp implements [help.KeyMap].
func (b *Background) FullHelp() [][]key.Binding {
	return [][]key.Binding{{b.keyMap.Stop, b.keyMap.Next, b.keyMap.Previous, b.keyMap.Close}}
}

// Finished implements list.Item.
func (i *BackgroundItem) Finished() bool { return true }

// ID implements list.Item.
func (i *BackgroundItem) ID() string {
	if i.task != nil {
		return i.task.ID
	}
	return strconv.Itoa(i.proc.PID)
}

// Filter implements list.FilterableItem.
func (i *BackgroundItem) Filter() string { return i.title() }

func (i *BackgroundItem) title() string {
	if i.task != nil {
		return "◐ " + i.task.Name
	}
	return "⚙ " + i.proc.Command
}

func (i *BackgroundItem) info() string {
	if i.task != nil {
		return fmt.Sprintf("sub-agent · %s/%s · %s", i.task.CLI, i.task.Model, since(i.task.Started))
	}
	return fmt.Sprintf("pid %d · %s", i.proc.PID, since(i.proc.Started))
}

func since(t time.Time) string {
	d := time.Since(t).Round(time.Second)
	if d >= time.Hour {
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	}
	return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
}

// SetFocused sets the focus state of the item.
func (i *BackgroundItem) SetFocused(focused bool) {
	if i.focused == focused {
		return
	}
	i.cache = nil
	i.focused = focused
	i.Bump()
}

// SetMatch sets the fuzzy match for the item.
func (i *BackgroundItem) SetMatch(m fuzzy.Match) {
	if sameFuzzyMatch(i.m, m) {
		return
	}
	i.cache = nil
	i.m = m
	i.Bump()
}

// Render returns the string representation of the item.
func (i *BackgroundItem) Render(width int) string {
	return renderItem(ListItemStyles{
		ItemBlurred:     i.t.Dialog.NormalItem,
		ItemFocused:     i.t.Dialog.SelectedItem,
		InfoTextBlurred: i.t.Dialog.ListItem.InfoBlurred,
		InfoTextFocused: i.t.Dialog.ListItem.InfoFocused,
	}, i.title(), i.info(), i.focused, width, i.cache, &i.m)
}

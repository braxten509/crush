package chat

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/crush/internal/agent"
	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/ui/list"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/charmbracelet/x/ansi"
)

// toolGroupShown is how many of the latest steps an expanded group shows.
const toolGroupShown = 5

// ToolGroupItem folds a run of tool calls (and the thinking-only steps
// between them) into one status line that shows the latest action.
// Clicking it shows the last few steps in full.
type ToolGroupItem struct {
	*list.Versioned
	*highlightableMessageItem
	*focusableMessageItem

	sty      *styles.Styles
	children []MessageItem
	expanded bool
	// live marks the group at the end of the chat, where the model may
	// still be working: it shows the latest step instead of a summary.
	live bool
	// Line where each shown child starts in the expanded render, for
	// routing clicks to it.
	childLines []int
	shown      []MessageItem
}

var (
	_ MessageItem = (*ToolGroupItem)(nil)
	_ Expandable  = (*ToolGroupItem)(nil)
	_ Animatable  = (*ToolGroupItem)(nil)
)

// NewToolGroupItem returns an empty group; SetChildren fills it.
func NewToolGroupItem(sty *styles.Styles) *ToolGroupItem {
	v := list.NewVersioned()
	return &ToolGroupItem{
		Versioned:                v,
		highlightableMessageItem: defaultHighlighter(sty, v),
		focusableMessageItem:     newFocusableMessageItem(v),
		sty:                      sty,
	}
}

// Foldable reports whether item belongs in a status group: tool calls
// other than the ones that need the user or show their own sub-steps,
// and assistant steps that only think.
func Foldable(item MessageItem) bool {
	switch it := item.(type) {
	case *AssistantMessageItem:
		return it.onlyThinking()
	case ToolMessageItem:
		switch it.ToolCall().Name {
		case tools.QuestionToolName, agent.AgentToolName, tools.AgenticFetchToolName:
			return false
		}
		return true
	}
	return false
}

// GroupsTools reports whether a run of foldable items needs a group: one
// made only of thinking steps renders as it always has.
func GroupsTools(run []MessageItem) bool {
	for _, it := range run {
		if _, ok := it.(ToolMessageItem); ok {
			return true
		}
	}
	return false
}

// ID implements MessageItem. A group is named after its first step.
func (g *ToolGroupItem) ID() string {
	if len(g.children) == 0 {
		return "toolgroup:"
	}
	return "toolgroup:" + g.children[0].ID()
}

// Children returns the steps in the group.
func (g *ToolGroupItem) Children() []MessageItem {
	return g.children
}

// SetChildren replaces the steps in the group.
func (g *ToolGroupItem) SetChildren(children []MessageItem) {
	g.children = children
	g.syncCompact()
	g.Bump()
}

// SetLive sets whether the group is at the end of the chat.
func (g *ToolGroupItem) SetLive(live bool) {
	if g.live != live {
		g.live = live
		g.Bump()
	}
}

// Child returns the step with the given ID, or nil.
func (g *ToolGroupItem) Child(id string) MessageItem {
	for _, c := range g.children {
		if c.ID() == id {
			return c
		}
	}
	return nil
}

// Version implements list.Item. Steps change on their own, so the group's
// version moves whenever any of theirs does.
func (g *ToolGroupItem) Version() uint64 {
	v := g.Versioned.Version()
	for _, c := range g.children {
		v += c.Version()
	}
	return v
}

// Finished implements list.Item.
func (g *ToolGroupItem) Finished() bool {
	for _, c := range g.children {
		if !c.Finished() {
			return false
		}
	}
	return true
}

// Spinning implements Animatable.
func (g *ToolGroupItem) Spinning() bool {
	for _, c := range g.children {
		if a, ok := c.(Animatable); ok && a.Spinning() {
			return true
		}
	}
	return false
}

// Advance implements Animatable. Steps bump their own versions, which the
// group's version includes.
func (g *ToolGroupItem) Advance() bool {
	changed := false
	for _, c := range g.children {
		if a, ok := c.(Animatable); ok && a.Spinning() && a.Advance() {
			changed = true
		}
	}
	return changed
}

// ToggleExpanded implements Expandable.
func (g *ToolGroupItem) ToggleExpanded() bool {
	g.expanded = !g.expanded
	g.syncCompact()
	g.Bump()
	return g.expanded
}

// syncCompact renders tools as one-line headers while collapsed (only the
// latest shows) and in full while expanded.
func (g *ToolGroupItem) syncCompact() {
	for _, c := range g.children {
		if cp, ok := c.(Compactable); ok {
			cp.SetCompact(!g.expanded)
		}
	}
}

// HandleMouseClick implements list.MouseClickable. The status line toggles
// the group; a click on a shown step toggles that step instead.
func (g *ToolGroupItem) HandleMouseClick(btn ansi.MouseButton, x, y int) bool {
	if btn != ansi.MouseLeft {
		return false
	}
	if !g.expanded || y == 0 {
		return true
	}
	for i := len(g.childLines) - 1; i >= 0; i-- {
		if y >= g.childLines[i] {
			c := g.shown[i]
			if cl, ok := c.(list.MouseClickable); ok && cl.HandleMouseClick(btn, x, y-g.childLines[i]) {
				if e, ok := c.(Expandable); ok {
					e.ToggleExpanded()
				}
			}
			return false
		}
	}
	return false
}

// RawRender implements MessageItem.
func (g *ToolGroupItem) RawRender(width int) string {
	inner := width - MessageLeftPaddingTotal
	var sb strings.Builder
	sb.WriteString(g.header(inner))
	g.childLines, g.shown = g.childLines[:0], g.shown[:0]
	if g.expanded {
		start := max(0, len(g.children)-toolGroupShown)
		if start > 0 {
			sb.WriteString("\n" + g.sty.Tool.ParamKey.Render(fmt.Sprintf("  … %d earlier", start)))
		}
		for _, c := range g.children[start:] {
			sb.WriteString("\n\n")
			g.childLines = append(g.childLines, strings.Count(sb.String(), "\n"))
			g.shown = append(g.shown, c)
			sb.WriteString(c.RawRender(width))
		}
	}
	content := sb.String()
	return g.renderHighlighted(content, inner, strings.Count(content, "\n")+1)
}

// Render implements list.Item.
func (g *ToolGroupItem) Render(width int) string {
	prefix := g.sty.Messages.ToolCallBlurred.Render()
	if g.focused {
		prefix = g.sty.Messages.ToolCallFocused.Render()
	}
	lines := strings.Split(g.RawRender(width), "\n")
	for i, ln := range lines {
		lines[i] = prefix + ln
	}
	return strings.Join(lines, "\n")
}

// header is the status line: the latest step while working, a summary
// once done.
func (g *ToolGroupItem) header(width int) string {
	marker := "▸ "
	if g.expanded {
		marker = "▾ "
	}
	var tools, failed int
	var edited []string
	seen := map[string]bool{}
	var latest MessageItem
	for _, c := range g.children {
		t, ok := c.(ToolMessageItem)
		if !ok {
			continue
		}
		tools++
		latest = c
		if st, ok := t.(interface{ computeStatus() ToolStatus }); ok && st.computeStatus() == ToolStatusError {
			failed++
		}
		if f := editedFile(t); f != "" && !seen[f] {
			seen[f] = true
			edited = append(edited, filepath.Base(f))
		}
	}
	count := fmt.Sprintf("%d action", tools)
	if tools != 1 {
		count += "s"
	}

	var line string
	switch last := g.children[len(g.children)-1]; {
	case g.live && !g.expanded:
		// Working: show what is happening right now.
		if a, ok := last.(*AssistantMessageItem); ok && a.isSpinning() {
			line = a.renderSpinning()
		} else if latest != nil {
			line, _, _ = strings.Cut(latest.RawRender(width+MessageLeftPaddingTotal), "\n")
		}
		line += g.sty.Tool.ParamKey.Render(" · " + count)
	default:
		icon := g.sty.Tool.IconSuccess.Render()
		if g.Spinning() {
			icon = g.sty.Tool.IconPending.Render()
		} else if failed > 0 {
			icon = g.sty.Tool.IconError.Render()
		}
		line = icon + " " + g.sty.Tool.NameNormal.Render(count)
		if len(edited) > 0 {
			files := strings.Join(edited[:min(3, len(edited))], ", ")
			if len(edited) > 3 {
				files += fmt.Sprintf(" +%d", len(edited)-3)
			}
			line += g.sty.Tool.ParamKey.Render(" · edited " + files)
		}
		if failed > 0 {
			line += g.sty.Tool.ErrorMessage.Render(fmt.Sprintf(" · %d failed", failed))
		}
	}
	return ansi.Truncate(g.sty.Tool.ParamKey.Render(marker)+line, width, "…")
}

// editedFile returns the file a file-changing tool touched, or "".
func editedFile(t ToolMessageItem) string {
	tc := t.ToolCall()
	switch tc.Name {
	case tools.EditToolName, tools.MultiEditToolName, tools.WriteToolName:
	default:
		return ""
	}
	var p struct {
		FilePath string `json:"file_path"`
	}
	_ = json.Unmarshal([]byte(tc.Input), &p)
	return p.FilePath
}

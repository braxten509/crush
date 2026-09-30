package chat

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/crush/internal/agent"
	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/ui/anim"
	"github.com/charmbracelet/crush/internal/ui/list"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/charmbracelet/x/ansi"
)

// toolGroupShown is how many of the latest steps an expanded group shows.
const toolGroupShown = 5

// ToolGroupItem folds a run of tool calls (and the thinking-only steps
// between them) into one status line that says what it is doing.
// Clicking it shows the last few steps in full.
type ToolGroupItem struct {
	*list.Versioned
	*highlightableMessageItem
	*focusableMessageItem

	sty      *styles.Styles
	children []MessageItem
	expanded bool
	// live marks the group at the end of the chat, where the model may
	// still be working: it says what is happening right now.
	live bool
	// busy is whether the agent is still working, so a live group keeps
	// a status between steps (a tool finished, the next hasn't started).
	busy bool
	// Line where each shown child starts in the expanded render, for
	// routing clicks to it.
	childLines []int
	shown      []MessageItem
	// anim animates the "| Running a command" status while working.
	anim      *anim.Anim
	animLabel string
	// canBackground is whether the agent can move a running command to
	// the background (Ctrl+B). The group times the command it shows to
	// offer that once it runs long.
	canBackground bool
	runningID     string
	runningSince  time.Time
	backgrounded  string // the command Ctrl+B was pressed for
	hinted        bool
}

// backgroundHintAfter is how long a command runs before the group offers
// Ctrl+B, as Claude Code does.
const backgroundHintAfter = 60 * time.Second

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
		anim: anim.New(anim.Settings{
			Size:        3,
			GradColorA:  sty.WorkingGradFromColor,
			GradColorB:  sty.WorkingGradToColor,
			LabelColor:  sty.WorkingLabelColor,
			CycleColors: true,
		}),
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

// SetBusy sets whether the agent is still working.
func (g *ToolGroupItem) SetBusy(busy bool) {
	if g.busy != busy {
		g.busy = busy
		g.Bump()
	}
}

// Child returns the step with the given ID, or nil.
// SetCanBackground says whether Ctrl+B can move the agent's running command
// to the background.
func (g *ToolGroupItem) SetCanBackground(can bool) {
	if g.canBackground != can {
		g.canBackground = can
		g.Bump()
	}
}

// runningCommand returns the ID of the command the live group is waiting
// on, or "".
func (g *ToolGroupItem) runningCommand() string {
	if !g.live || len(g.children) == 0 {
		return ""
	}
	last, ok := g.children[len(g.children)-1].(ToolMessageItem)
	if !ok || last.ToolCall().Name != tools.BashToolName {
		return ""
	}
	if st, ok := last.(interface{ computeStatus() ToolStatus }); !ok || st.computeStatus() != ToolStatusRunning {
		return ""
	}
	return last.ToolCall().ID
}

// CanBackground reports whether Ctrl+B would move the group's running
// command to the background.
func (g *ToolGroupItem) CanBackground() bool {
	id := g.runningCommand()
	if id != g.runningID {
		g.runningID, g.runningSince = id, time.Now()
	}
	return g.canBackground && id != "" && id != g.backgrounded
}

// BackgroundHint reports whether the group shows the Ctrl+B hint: its
// command has run for [backgroundHintAfter].
func (g *ToolGroupItem) BackgroundHint() bool {
	return g.CanBackground() && time.Since(g.runningSince) >= backgroundHintAfter
}

// Backgrounded hides the hint once Ctrl+B was pressed for the command.
func (g *ToolGroupItem) Backgrounded() {
	g.backgrounded = g.runningID
	g.Bump()
}

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
	if g.status() != "" {
		return true
	}
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
	if g.status() != "" && g.anim.Advance() {
		g.Bump()
		changed = true
	}
	if hint := g.BackgroundHint(); hint != g.hinted {
		g.hinted = hint
		g.Bump()
		changed = true
	}
	return changed
}

// status says what the model is doing right now ("Thinking", "Running a
// command"), or "" when the group isn't working.
func (g *ToolGroupItem) status() string {
	if !g.live || len(g.children) == 0 {
		return ""
	}
	switch last := g.children[len(g.children)-1].(type) {
	case *AssistantMessageItem:
		if last.isSpinning() {
			return "Thinking"
		}
	case ToolMessageItem:
		st, ok := last.(interface{ computeStatus() ToolStatus })
		if !ok {
			return ""
		}
		switch st.computeStatus() {
		case ToolStatusAwaitingPermission:
			return "Waiting for approval"
		case ToolStatusRunning:
			return toolActivity(last.ToolCall().Name)
		}
	}
	if g.busy {
		// The last step is done but the agent isn't: the model is
		// working out what to do next.
		return "Thinking"
	}
	return ""
}

// toolActivity describes a running tool in a few words.
func toolActivity(name string) string {
	switch name {
	case tools.BashToolName:
		return "Running a command"
	case tools.JobOutputToolName, tools.JobKillToolName:
		return "Checking a command"
	case tools.ViewToolName:
		return "Reading a file"
	case tools.EditToolName, tools.MultiEditToolName, tools.WriteToolName:
		return "Editing a file"
	case tools.GrepToolName, tools.GlobToolName, tools.LSToolName, tools.SourcegraphToolName:
		return "Searching"
	case tools.WebSearchToolName:
		return "Searching the web"
	case tools.FetchToolName, tools.WebFetchToolName, tools.DownloadToolName:
		return "Reading a web page"
	case tools.TodosToolName:
		return "Updating the plan"
	}
	if strings.HasPrefix(name, "lsp_") {
		return "Checking code"
	}
	if server, _, ok := strings.Cut(strings.TrimPrefix(name, "mcp_"), "_"); ok && strings.HasPrefix(name, "mcp_") {
		return "Using " + server
	}
	return "Working"
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
	if g.BackgroundHint() {
		sb.WriteString("\n" + g.sty.Tool.ParamKey.Render("  ctrl+b to run in background"))
	}
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

// header is the status line: the action count, plus what is happening
// now while working or a summary once done.
func (g *ToolGroupItem) header(width int) string {
	marker := "▸ "
	if g.expanded {
		marker = "▾ "
	}
	var tools, failed, moved int
	var edited []string
	seen := map[string]bool{}
	for _, c := range g.children {
		t, ok := c.(ToolMessageItem)
		if !ok {
			continue
		}
		tools++
		if st, ok := t.(interface{ computeStatus() ToolStatus }); ok && st.computeStatus() == ToolStatusError {
			failed++
		}
		if isMovedToBackground(t) {
			moved++
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

	status := g.status()
	icon := g.sty.Tool.IconSuccess.Render()
	if status != "" || g.Spinning() {
		icon = g.sty.Tool.IconPending.Render()
	} else {
		// Check only the last tool's status for the icon, skipping thinking-only steps
		for i := len(g.children) - 1; i >= 0; i-- {
			if lastTool, ok := g.children[i].(interface{ computeStatus() ToolStatus }); ok {
				if lastTool.computeStatus() == ToolStatusError {
					icon = g.sty.Tool.IconError.Render()
				} else if t, ok := g.children[i].(ToolMessageItem); ok && isMovedToBackground(t) {
					// Still running, so not a finished ✓.
					icon = g.sty.Tool.IconPending.SetString(backgroundIcon).Render()
				}
				break
			}
		}
	}
	line := icon + " " + g.sty.Tool.NameNormal.Render(count)
	if len(edited) > 0 && status == "" {
		files := strings.Join(edited[:min(3, len(edited))], ", ")
		if len(edited) > 3 {
			files += fmt.Sprintf(" +%d", len(edited)-3)
		}
		line += g.sty.Tool.ParamKey.Render(" · edited " + files)
	}
	if moved > 0 {
		line += g.sty.Tool.ParamKey.Render(fmt.Sprintf(" · %d backgrounded", moved))
	}
	if failed > 0 {
		line += g.sty.Tool.ErrorMessage.Render(fmt.Sprintf(" · %d failed", failed))
	}
	if status != "" {
		// Working: say what's happening now, not the whole command.
		if status != g.animLabel {
			g.animLabel = status
			g.anim.SetLabel(status)
		}
		line += g.sty.Tool.ParamKey.Render(" | ") + g.anim.Render()
	}
	return ansi.Truncate(g.sty.Tool.ParamKey.Render(marker)+line, width, "…")
}

// backgroundIcon marks background work, as in the background row.
const backgroundIcon = "⚙"

// isMovedToBackground reports whether the tool's command went on running
// in the background instead of finishing.
func isMovedToBackground(t ToolMessageItem) bool {
	r, ok := t.(interface{ Result() *message.ToolResult })
	return ok && movedToBackground(r.Result())
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

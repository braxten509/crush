package chat

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"time"

	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/diffreview"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/x/ansi"
)

// header is the status line: the action count, plus what is happening
// now while working or a summary once done.
func (g *ToolGroupItem) header(width int) string {
	marker := "▸ "
	if g.expanded {
		marker = "▾ "
	}
	tally := g.tallySteps()
	count := actionCount(tally.tools)

	status := g.status()
	icon := g.headerIcon(status)
	line := icon + " " + g.sty.Tool.NameNormal.Render(count)
	// The original action label, immediately after the status icon, opens
	// the review. File names and line counts remain a passive summary.
	g.changesCols = [2]int{}
	changes := g.Changes()
	storedSummary := g.StoredReviewSummary()
	if len(changes) > 0 || storedSummary != "" {
		start := ansi.StringWidth(marker + icon + " ")
		end := min(width, start+ansi.StringWidth(count))
		if end > start {
			g.changesCols = [2]int{MessageLeftPaddingTotal + start, MessageLeftPaddingTotal + end}
		}
	}
	line += g.changeSummary(status, changes, tally.edited, storedSummary)
	if tally.moved > 0 {
		line += g.sty.Tool.ParamKey.Render(fmt.Sprintf(" · %d backgrounded", tally.moved))
	}
	if tally.failed > 0 {
		line += g.sty.Tool.ErrorMessage.Render(fmt.Sprintf(" · %d failed", tally.failed))
	}
	line += g.activitySuffix(status)
	return ansi.Truncate(g.sty.Tool.ParamKey.Render(marker)+line, width, "…")
}

// stepTally counts a group's tool calls for its header.
type stepTally struct {
	tools, failed, moved int
	// edited holds the base names of the files the tools edited, once
	// each, in order.
	edited []string
}

// tallySteps counts the tool calls, the failed and backgrounded ones, and
// the files they edited.
func (g *ToolGroupItem) tallySteps() stepTally {
	var tally stepTally
	seen := map[string]bool{}
	for _, c := range g.children {
		t, ok := c.(ToolMessageItem)
		if !ok {
			continue
		}
		tally.tools++
		if st, ok := t.(interface{ computeStatus() ToolStatus }); ok && st.computeStatus() == ToolStatusError {
			tally.failed++
		}
		if isMovedToBackground(t) {
			tally.moved++
		}
		if f := editedFile(t); f != "" && !seen[f] && !ignoredReviewPath(f, "") {
			seen[f] = true
			tally.edited = append(tally.edited, filepath.Base(f))
		}
	}
	return tally
}

// headerIcon is pending while the group works, and otherwise follows the
// last tool's outcome.
func (g *ToolGroupItem) headerIcon(status string) string {
	if status != "" || g.Spinning() {
		return g.sty.Tool.IconPending.Render()
	}
	// Check only the last tool's status for the icon, skipping thinking-only steps
	for i := len(g.children) - 1; i >= 0; i-- {
		if lastTool, ok := g.children[i].(interface{ computeStatus() ToolStatus }); ok {
			if lastTool.computeStatus() == ToolStatusError {
				return g.sty.Tool.IconError.Render()
			}
			if t, ok := g.children[i].(ToolMessageItem); ok && isMovedToBackground(t) {
				// Still running, so not a finished ✓.
				return g.sty.Tool.IconPending.SetString(backgroundIcon).Render()
			}
			break
		}
	}
	return g.sty.Tool.IconSuccess.Render()
}

// changeSummary describes the files the group changed: the saved summary,
// the captured changes, or the files the tools edited, with line counts
// when known.
func (g *ToolGroupItem) changeSummary(status string, changes []diffreview.File, edited []string, storedSummary string) string {
	switch {
	case storedSummary != "":
		summary := g.sty.Tool.ParamKey.Render(" · " + storedSummary)
		if g.storedCounted {
			summary += " " + g.lineCounts(g.storedAdds, g.storedDels)
		}
		return summary
	case g.storedDeletions && len(changes) == 0:
		return g.sty.Tool.ParamKey.Render(" · Some files were deleted")
	case len(changes) > 0 || len(edited) > 0:
		return g.liveChangeSummary(status, changes, edited)
	}
	return ""
}

// liveChangeSummary describes changes made in this session. While working
// only line counts are shown, and only when there are some.
func (g *ToolGroupItem) liveChangeSummary(status string, changes []diffreview.File, edited []string) string {
	// Copies, moves and removals alone have no line counts to show.
	counted := diffreview.AnyCounted(changes) && slices.ContainsFunc(changes, diffreview.File.HasEdits)
	if status != "" && !counted {
		return ""
	}
	summary := g.sty.Tool.ParamKey.Render(" · ")
	if status == "" {
		label := g.liveChangeLabel(changes, edited)
		summary += g.sty.Tool.ParamKey.Render(label)
		if counted && label != "" {
			summary += " "
		}
	}
	if counted {
		adds, dels := diffreview.Stats(changes)
		summary += g.lineCounts(adds, dels)
	}
	return summary
}

// lineCounts renders added and removed line counts.
func (g *ToolGroupItem) lineCounts(adds, dels int) string {
	return g.sty.Tool.ChangesAdd.Render(fmt.Sprintf("+%d", adds)) + " " +
		g.sty.Tool.ChangesDel.Render(fmt.Sprintf("−%d", dels))
}

// activitySuffix keeps the activity timer and animation in step with
// status and renders what is happening now, or "" when idle.
func (g *ToolGroupItem) activitySuffix(status string) string {
	g.syncActivity(time.Now())
	if status != "" && status != g.animLabel {
		g.animLabel = status
		g.anim.SetLabel(status)
	}
	if status == "Compacting conversation" {
		return g.sty.Tool.ParamKey.Render(" | ") + renderCompactingStatus(g.sty, g.activity.elapsed(time.Now()), g.anim.RenderEllipsis())
	}
	if status != "" {
		// Working: say what's happening now, not the whole command.
		return g.sty.Tool.ParamKey.Render(" | ") + g.anim.Render() + renderThoughtLine(g.sty, g.thoughtLine(status))
	}
	return ""
}

// liveChangeLabel describes the captured file changes or, without them,
// the files the tools edited. Shells, MCP tools and failed commands can
// change files too, so captured changes win.
func (g *ToolGroupItem) liveChangeLabel(changes []diffreview.File, edited []string) string {
	if len(changes) == 0 {
		name := ""
		if len(edited) == 1 {
			name = edited[0]
		}
		return fileChangeLabel(map[string]int{"edited": len(edited)}, name)
	}
	kinds := map[string]int{}
	seen := map[string]bool{}
	name := ""
	for _, change := range changes {
		if seen[change.Path] {
			continue
		}
		seen[change.Path] = true
		kinds[changeKind(change)]++
		name = filepath.Base(change.Path)
	}
	return fileChangeLabel(kinds, name)
}

// changeKind is the verb for what happened to one file.
func changeKind(f diffreview.File) string {
	if f.Kind == diffreview.Deleted {
		return "removed"
	}
	switch f.Transfer {
	case "checkout":
		return "checked out"
	case "copy":
		return "copied"
	case "move":
		return "moved"
	case "generated":
		return "generated"
	}
	return "edited"
}

// maxFileNameWidth caps a file name in the summary so a long one can't take
// over the line.
const maxFileNameWidth = 24

// fileChangeLabel names one file with what happened to it, counts several
// files of one kind ("moved 3 files"), and calls a mix "N files changed".
// name is the file's name when there is exactly one, else ignored.
func fileChangeLabel(kinds map[string]int, name string) string {
	total, verb := 0, ""
	for kind, n := range kinds {
		if n == 0 {
			continue
		}
		total += n
		if verb == "" {
			verb = kind
		} else {
			verb = "mixed"
		}
	}
	switch {
	case total == 0:
		return ""
	case total == 1 && name != "":
		return verb + " " + shortFileName(name)
	case total == 1:
		return verb + " 1 file"
	case verb != "mixed":
		return fmt.Sprintf("%s %d files", verb, total)
	}
	return fmt.Sprintf("%d files changed", total)
}

// shortFileName keeps a file name within maxFileNameWidth, cutting the
// middle so its start and extension stay.
func shortFileName(name string) string {
	if ansi.StringWidth(name) <= maxFileNameWidth {
		return name
	}
	tail := 8
	head := maxFileNameWidth - tail - 1
	return ansi.Truncate(name, head, "") + "…" + ansi.TruncateLeft(name, ansi.StringWidth(name)-tail, "")
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
	if result, ok := t.(interface{ Result() *message.ToolResult }); ok && result.Result() != nil {
		if review := result.Result().Review; review != nil {
			if review.Summary != nil && review.Summary.Files == 0 || review.Summary == nil && len(review.Changes) == 0 {
				return ""
			}
		}
	}
	tc := t.ToolCall()
	switch tc.Name {
	case tools.EditToolName, tools.MultiEditToolName, tools.WriteToolName:
	default:
		return ""
	}
	var p struct {
		FilePath   string `json:"file_path"`
		DeleteFile bool   `json:"delete_file"`
	}
	_ = json.Unmarshal([]byte(tc.Input), &p)
	if p.DeleteFile {
		return ""
	}
	return p.FilePath
}

// thoughtLine is the newest line of the live step's thinking while the
// group is thinking, or "".
func (g *ToolGroupItem) thoughtLine(status string) string {
	if status != "Thinking" || len(g.children) == 0 {
		return ""
	}
	if last, ok := g.children[len(g.children)-1].(*AssistantMessageItem); ok {
		return thoughtLine(last.message)
	}
	return ""
}

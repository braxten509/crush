package dialog

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"context"
	"fmt"
	"github.com/charmbracelet/crush/internal/filehistory"
	"github.com/charmbracelet/crush/internal/ui/common"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"strings"
)

const FileRestoreID = "file-restore"

type ActionUndoFiles struct{}
type ActionFileRestore struct {
	Plan    *filehistory.Plan
	Next    Action
	Restore bool
}
type FileRestore struct {
	com            *common.Common
	plan           *filehistory.Plan
	next           Action
	choice, offset int
	maximumOffset  int
	working        string
	cancel         context.CancelFunc
}

func NewFileRestore(com *common.Common, plan *filehistory.Plan, next Action) *FileRestore {
	return &FileRestore{com: com, plan: plan, next: next}
}
func NewFileRestoreBusy(com *common.Common, text string, cancel context.CancelFunc) *FileRestore {
	return &FileRestore{com: com, working: text, cancel: cancel}
}
func (*FileRestore) ID() string { return FileRestoreID }
func (d *FileRestore) HandleMsg(msg tea.Msg) Action {
	if d.working != "" {
		if k, ok := msg.(tea.KeyPressMsg); ok && k.String() == "esc" && d.cancel != nil {
			d.cancel()
			return ActionClose{}
		}
		return nil
	}
	if k, ok := msg.(tea.KeyPressMsg); ok {
		switch k.String() {
		case "esc":
			return ActionClose{}
		case "right", "tab":
			d.choice = (d.choice + 1) % 3
		case "left", "shift+tab":
			d.choice = (d.choice + 2) % 3
		case "up":
			d.offset = max(0, d.offset-1)
		case "down":
			d.offset = min(d.maximumOffset, d.offset+1)
		case "enter":
			if d.choice == 2 {
				return ActionClose{}
			}
			return ActionFileRestore{d.plan, d.next, d.choice == 0}
		}
	}
	return nil
}
func (d *FileRestore) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := d.com.Styles
	width := max(0, min(94, area.Dx()-t.Dialog.View.GetHorizontalBorderSize()))
	inner := max(1, width-t.Dialog.View.GetHorizontalFrameSize()-t.Dialog.List.GetHorizontalFrameSize())
	if d.working != "" {
		rc := NewRenderContext(t, width)
		rc.Title = d.working
		hint := "Please wait."
		if d.cancel != nil {
			hint = "esc cancel"
		}
		rc.AddPart(t.Dialog.List.Height(2).Render(hint))
		DrawCenterCursor(scr, area, rc.Render(), nil)
		return nil
	}
	var lines []string
	for _, e := range d.plan.Entries {
		status := ""
		if e.Unavailable != "" {
			status = "SKIP: " + e.Unavailable
		} else if e.Conflict != "" {
			status = "CONFLICT: " + e.Conflict
		}
		row := fmt.Sprintf("%-8s +%d -%d", e.Action, e.Adds, e.Dels)
		if status != "" {
			row += " · " + status
		}
		lines = append(lines, strings.Split(ansi.Hardwrap(row, inner, true), "\n")...)
		lines = append(lines, strings.Split(ansi.Hardwrap(e.Path, inner, true), "\n")...)
	}
	notes := []string{"Conflicted and unavailable files are skipped.", "Only tracked files change. Other edits, databases, installed packages, network effects and Git commits are not undone."}
	moved := false
	for _, e := range d.plan.Entries {
		moved = moved || e.GitMoved
	}
	if moved {
		notes = append(notes, "Git HEAD moved: restored files will be uncommitted changes.")
	} else {
		notes = append(notes, "You can undo this restore from the command palette.")
	}
	for _, note := range notes {
		lines = append(lines, strings.Split(ansi.Wrap(note, inner, ""), "\n")...)
	}
	buttons := []string{"Restore files", "Chat only", "Cancel"}
	if d.plan.UndoID != "" {
		buttons[0] = "Undo restore"
		buttons[1] = "Keep files"
	}
	for i := range buttons {
		if i == d.choice {
			buttons[i] = "[" + buttons[i] + "]"
		} else {
			buttons[i] = " " + buttons[i] + " "
		}
	}
	actions := strings.Join(buttons, "  ")
	if ansi.StringWidth(actions) > inner {
		actions = strings.Join(buttons, "\n")
	}
	rc := NewRenderContext(t, width)
	rc.Title = fmt.Sprintf("Restore files? · %d files", len(d.plan.Entries))
	if len(d.plan.Entries) == 1 {
		rc.Title = "Restore files? · 1 file"
	}
	if d.plan.UndoID != "" {
		rc.Title = "Undo last file restore?"
	}
	rc.Title = ansi.Truncate(rc.Title, inner, "…")
	footer := t.Dialog.List.Render(actions)
	hint := t.Dialog.List.Render(ansi.Truncate("↑↓ scroll  ←→ choose  enter confirm  esc cancel", inner, "…"))
	// Size from the actual shared styles so compact terminals retain all buttons.
	rc.AddPart(footer)
	rc.AddPart(hint)
	overhead := lipgloss.Height(rc.Render()) + t.Dialog.List.GetVerticalFrameSize()
	height := max(1, min(len(lines), 16, area.Dy()-overhead))
	d.maximumOffset = max(0, len(lines)-height)
	d.offset = min(d.offset, d.maximumOffset)
	end := min(len(lines), d.offset+height)
	body := t.Dialog.List.Height(height).Render(strings.Join(lines[d.offset:end], "\n"))
	rc.Parts = []string{body, footer, hint}
	DrawCenterCursor(scr, area, rc.Render(), nil)
	return nil
}

package questionform

import (
	"fmt"
	"image"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/question"
	"github.com/charmbracelet/crush/internal/ui/styles"
	uv "github.com/charmbracelet/ultraviolet"
)

// Height returns the total height using the max tab height so
// switching tabs doesn't cause layout jumps.
func (f *QuestionForm) Height(width int) int {
	h := 0
	if f.showTabs {
		h = 4 // bordered tab row (top + label + bottom) + blank line
	}
	maxQ := 0
	for _, q := range f.questions {
		if qh := q.Height(width); qh > maxQ {
			maxQ = qh
		}
	}
	if f.confirmComp != nil {
		if ch := f.confirmComp.Height(width); ch > maxQ {
			maxQ = ch
		}
	}
	h += maxQ
	return h
}

// CollapsedHeight returns the height of the collapsed summary
// line shown when the editor area is not focused.
func (f *QuestionForm) CollapsedHeight() int { return 1 }

// ShouldCollapse reports whether the form should use its compact summary
// while the chat has focus.
func (f *QuestionForm) ShouldCollapse(width, terminalHeight int) bool {
	return terminalHeight > 0 && f.Height(width) > terminalHeight*2/5
}

// CollapsedHelp returns the help description for restoring the form.
func (f *QuestionForm) CollapsedHelp() string { return "answer questions" }

// DrawCollapsed renders a compact one-line summary of the form
// when the user has tabbed away to the chat. For multi-question
// batches it shows the active question text and answered count;
// for single questions it shows just the question text.
func (f *QuestionForm) DrawCollapsed(scr uv.Screen, area uv.Rectangle) {
	// Folded away, the form has no room for a sketch.
	for _, q := range f.questions {
		if cl, ok := q.(interface{ clearPlacement() }); ok {
			cl.clearPlacement()
		}
	}
	// The full form isn't on screen, so there is nothing to select.
	f.sel.clear()
	f.sel.area = image.Rectangle{}
	icon := questionIconPrompt(f.Styles, false)
	iconWidth := lipgloss.Width(icon)
	textStyle := f.Styles.Messages.AssistantInfoModel
	countStyle := f.Styles.Messages.AssistantInfoProvider
	lineStyle := f.Styles.Section.Line

	var plainText string
	var confirmRendered string
	if f.numQuestions > 1 {
		answered := 0
		for i := 0; i < f.numQuestions; i++ {
			if f.isAnswered(i) {
				answered++
			}
		}
		if f.isConfirmTab() && f.confirmComp != nil {
			plainText = f.confirmComp.Title
			confirmRendered = f.Styles.Editor.QuestionUnselected.Render(f.confirmComp.Title)
		} else if f.activeIdx < len(f.questions) {
			plainText = f.getQuestionText(f.activeIdx)
		}
		count := fmt.Sprintf("(%d/%d answered)", answered, f.numQuestions)
		plainLabel := plainText + " " + count
		textWidth := iconWidth + 1 + lipgloss.Width(plainLabel)
		remaining := area.Dx() - textWidth - 1

		var rendered string
		if confirmRendered != "" {
			rendered = fmt.Sprintf("%s%s %s", icon, confirmRendered, countStyle.Render(count))
		} else {
			rendered = fmt.Sprintf("%s%s %s", icon, textStyle.Render(plainText), countStyle.Render(count))
		}
		if remaining > 0 {
			rendered = rendered + " " + lineStyle.Render(strings.Repeat(styles.SectionSeparator, remaining))
		}
		drawStyledText(scr, area, rendered)
	} else if f.numQuestions == 1 {
		plainText = f.getQuestionText(0)
		textWidth := iconWidth + 1 + lipgloss.Width(plainText)
		remaining := area.Dx() - textWidth - 1
		rendered := fmt.Sprintf("%s%s", icon, textStyle.Render(plainText))
		if remaining > 0 {
			rendered = rendered + " " + lineStyle.Render(strings.Repeat(styles.SectionSeparator, remaining))
		}
		drawStyledText(scr, area, rendered)
	}
}

// getQuestionText returns the question text for the given index.
func (f *QuestionForm) getQuestionText(idx int) string {
	type hasRequest interface {
		GetRequest() question.Question
	}
	if idx < len(f.questions) {
		if hr, ok := f.questions[idx].(hasRequest); ok {
			return hr.GetRequest().Text
		}
	}
	if idx < len(f.labels) {
		return f.labels[idx]
	}
	return ""
}

// Draw renders the tab bar and the active tab content, then records
// the drawn cells for mouse selection and paints any highlight.
func (f *QuestionForm) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	cur := f.drawForm(scr, area)
	f.sel.capture(scr, area, f.Styles.TextSelection)
	return cur
}

// drawForm renders the tab bar and the active tab content. When
// showTabs is false (single question), renders content directly
// without tab chrome.
func (f *QuestionForm) drawForm(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	contentY := area.Min.Y

	if f.showTabs {
		contentY = f.drawTabBar(scr, area)
	} else {
		f.compositor = nil
	}

	contentArea := image.Rect(area.Min.X, contentY, area.Max.X, area.Max.Y)

	if f.isConfirmTab() {
		return f.confirmComp.Draw(scr, contentArea)
	}
	if f.activeIdx < f.numQuestions {
		cur := f.questions[f.activeIdx].Draw(scr, contentArea)
		if cur != nil {
			cur.Y += contentY - area.Min.Y
		}
		return cur
	}
	return nil
}

// HeightChanged reports whether any component's height changed.
func (f *QuestionForm) HeightChanged() bool {
	for _, q := range f.questions {
		if q.HeightChanged() {
			return true
		}
	}
	if f.confirmComp != nil && f.confirmComp.HeightChanged() {
		return true
	}
	return false
}

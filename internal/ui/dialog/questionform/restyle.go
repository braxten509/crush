package questionform

import "github.com/charmbracelet/crush/internal/ui/dialog"

// RefreshStyles implements [dialog.StyleRefresher].
func (f *QuestionForm) RefreshStyles() {
	for _, q := range f.questions {
		if r, ok := q.(dialog.StyleRefresher); ok {
			r.RefreshStyles()
		}
	}
}

// RefreshStyles implements [dialog.StyleRefresher].
func (e *questionEditor) RefreshStyles() {
	e.fillIn.SetStyles(questionTextareaStyles(e.Styles))
	e.noteEditor.SetStyles(questionTextareaStyles(e.Styles))
}

// RefreshStyles implements [dialog.StyleRefresher].
func (d *FreeText) RefreshStyles() {
	d.editor.SetStyles(questionTextareaStyles(d.Styles))
}

// RefreshStyles implements [dialog.StyleRefresher].
func (p *PlanHandoffInline) RefreshStyles() {
	p.editor.SetStyles(questionTextareaStyles(p.com.Styles))
}

package diffview

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/aymanbagabas/go-udiff"
	"github.com/charmbracelet/x/ansi"
)

// diffRows collects the rendered rows of a diff view, dropping the first
// yOffset of them.
type diffRows struct {
	b       strings.Builder
	printed int
}

// visible reports whether the next row is shown, i.e. scrolled into view.
func (r *diffRows) visible() bool { return r.printed >= 0 }

// add renders a row with draw when it is visible, and counts it either way.
func (r *diffRows) add(draw func(b *strings.Builder)) {
	if r.visible() {
		draw(&r.b)
		r.b.WriteByte('\n')
	}
	r.printed++
}

// cutsOff reports whether the line about to be printed has to become an
// ellipsis because the view is full and more lines would follow it.
func (dv *DiffView) cutsOff(printed int, isLastHunk, isLastLine bool) bool {
	hasReachedHeight := dv.height > 0 && printed+1 == dv.height
	return hasReachedHeight && (!isLastHunk || !isLastLine)
}

// lineContent highlights a line of code, scrolls it horizontally and fits
// it to the code width. leadingEllipsis says whether text was cut on the
// left.
func (dv *DiffView) lineContent(in string, ls LineStyle) (content string, leadingEllipsis bool) {
	content = strings.TrimSuffix(in, "\n")
	content = dv.hightlightCode(content, ls.Code.GetBackground())
	content = ansi.GraphemeWidth.Cut(content, dv.xOffset, len(content))
	content = ansi.Truncate(content, dv.codeWidth, "…")
	leadingEllipsis = dv.xOffset > 0 && strings.TrimSpace(content) != ""
	return content, leadingEllipsis
}

// beforeNumber writes the before-side line number column, if shown.
func (dv *DiffView) beforeNumber(b *strings.Builder, ls LineStyle, v any) {
	if dv.lineNumbers {
		b.WriteString(ls.LineNumber.Render(pad(v, dv.beforeNumDigits)))
	}
}

// afterNumber writes the after-side line number column, if shown.
func (dv *DiffView) afterNumber(b *strings.Builder, ls LineStyle, v any) {
	if dv.lineNumbers {
		b.WriteString(ls.LineNumber.Render(pad(v, dv.afterNumDigits)))
	}
}

// renderUnified renders the unified diff view as a string.
func (dv *DiffView) renderUnified() string {
	rows := diffRows{printed: -dv.yOffset}
	fullContentStyle := lipgloss.NewStyle().MaxWidth(dv.fullCodeWidth)

	for i, h := range dv.unified.Hunks {
		// Render file name header before the first hunk.
		if i == 0 && dv.fileName != "" {
			rows.add(func(b *strings.Builder) {
				dv.unifiedHeaderRow(b, dv.style.Filename, "  "+dv.fileName)
			})
		}
		rows.add(func(b *strings.Builder) {
			dv.unifiedHeaderRow(b, dv.style.DividerLine, dv.hunkLineFor(h))
		})

		beforeLine := h.FromLine
		afterLine := h.ToLine

		for j, l := range h.Lines {
			// print ellipis if we don't have enough space to print the rest of the diff
			if dv.cutsOff(rows.printed, i+1 == len(dv.unified.Hunks), j+1 == len(h.Lines)) {
				if rows.visible() {
					ls := dv.lineStyleForType(l.Kind)
					dv.beforeNumber(&rows.b, ls, "…")
					dv.afterNumber(&rows.b, ls, "…")
					rows.b.WriteString(fullContentStyle.Render(
						ls.Code.Width(dv.fullCodeWidth).Render("  …"),
					))
					rows.b.WriteRune('\n')
				}
				return rows.b.String()
			}

			rows.add(func(b *strings.Builder) {
				dv.unifiedLine(b, fullContentStyle, l, beforeLine, afterLine)
			})
			switch l.Kind {
			case udiff.Equal:
				beforeLine++
				afterLine++
			case udiff.Insert:
				afterLine++
			case udiff.Delete:
				beforeLine++
			}
		}
	}

	return rows.b.String()
}

// unifiedHeaderRow writes a file name or hunk header row of the unified
// view.
func (dv *DiffView) unifiedHeaderRow(b *strings.Builder, ls LineStyle, text string) {
	dv.beforeNumber(b, ls, "…")
	dv.afterNumber(b, ls, "…")
	content := ansi.Truncate(text, dv.fullCodeWidth, "…")
	b.WriteString(ls.Code.Width(dv.fullCodeWidth).Render(content))
}

// unifiedLine writes one line of a hunk in the unified view.
func (dv *DiffView) unifiedLine(b *strings.Builder, fullContentStyle lipgloss.Style, l udiff.Line, beforeLine, afterLine int) {
	switch l.Kind {
	case udiff.Equal:
		ls := dv.style.EqualLine
		content, leadingEllipsis := dv.lineContent(l.Content, ls)
		dv.beforeNumber(b, ls, beforeLine)
		dv.afterNumber(b, ls, afterLine)
		b.WriteString(fullContentStyle.Render(
			ls.Code.Width(dv.fullCodeWidth).Render(ternary(leadingEllipsis, " …", "  ") + content),
		))
	case udiff.Insert:
		ls := dv.style.InsertLine
		content, leadingEllipsis := dv.lineContent(l.Content, ls)
		dv.beforeNumber(b, ls, " ")
		dv.afterNumber(b, ls, afterLine)
		b.WriteString(fullContentStyle.Render(
			ls.Symbol.Render(ternary(leadingEllipsis, "+…", "+ ")) +
				ls.Code.Width(dv.codeWidth).Render(content),
		))
	case udiff.Delete:
		ls := dv.style.DeleteLine
		content, leadingEllipsis := dv.lineContent(l.Content, ls)
		dv.beforeNumber(b, ls, beforeLine)
		dv.afterNumber(b, ls, " ")
		b.WriteString(fullContentStyle.Render(
			ls.Symbol.Render(ternary(leadingEllipsis, "-…", "- ")) +
				ls.Code.Width(dv.codeWidth).Render(content),
		))
	}
}

// splitStyles are the width limits of the two halves of the split view.
type splitStyles struct {
	before, after lipgloss.Style
}

// renderSplit renders the split (side-by-side) diff view as a string.
func (dv *DiffView) renderSplit() string {
	rows := diffRows{printed: -dv.yOffset}
	full := splitStyles{
		before: lipgloss.NewStyle().MaxWidth(dv.fullCodeWidth),
		after:  lipgloss.NewStyle().MaxWidth(dv.fullCodeWidth + btoi(dv.extraColOnAfter)),
	}

	for i, h := range dv.splitHunks {
		// Render file name header before the first hunk.
		if i == 0 && dv.fileName != "" {
			rows.add(func(b *strings.Builder) {
				dv.splitHeaderRow(b, dv.style.Filename, "  "+dv.fileName)
			})
		}
		rows.add(func(b *strings.Builder) {
			dv.splitHeaderRow(b, dv.style.DividerLine, dv.hunkLineFor(dv.unified.Hunks[i]))
		})

		beforeLine := h.fromLine
		afterLine := h.toLine

		for j, l := range h.lines {
			// print ellipis if we don't have enough space to print the rest of the diff
			if dv.cutsOff(rows.printed, i+1 == len(dv.unified.Hunks), j+1 == len(h.lines)) {
				if rows.visible() {
					dv.splitEllipsisRow(&rows.b, full, l)
				}
				return rows.b.String()
			}

			rows.add(func(b *strings.Builder) {
				dv.splitBefore(b, full.before, l.before, beforeLine)
				dv.splitAfter(b, full.after, l.after, afterLine)
			})
			if l.before != nil && (l.before.Kind == udiff.Equal || l.before.Kind == udiff.Delete) {
				beforeLine++
			}
			if l.after != nil && (l.after.Kind == udiff.Equal || l.after.Kind == udiff.Insert) {
				afterLine++
			}
		}
	}

	return rows.b.String()
}

// splitHeaderRow writes a file name or hunk header row of the split view:
// the text on the before side and an empty after side.
func (dv *DiffView) splitHeaderRow(b *strings.Builder, ls LineStyle, text string) {
	dv.beforeNumber(b, ls, "…")
	content := ansi.Truncate(text, dv.fullCodeWidth, "…")
	b.WriteString(ls.Code.Width(dv.fullCodeWidth).Render(content))
	dv.afterNumber(b, ls, "…")
	b.WriteString(ls.Code.Width(dv.fullCodeWidth + btoi(dv.extraColOnAfter)).Render(" "))
}

// splitEllipsisRow writes the row that stands for the lines that did not
// fit, styled after the line it replaces.
func (dv *DiffView) splitEllipsisRow(b *strings.Builder, full splitStyles, l *splitLine) {
	ls := dv.style.MissingLine
	if l.before != nil {
		ls = dv.lineStyleForType(l.before.Kind)
	}
	dv.beforeNumber(b, ls, "…")
	b.WriteString(full.before.Render(
		ls.Code.Width(dv.fullCodeWidth).Render("  …"),
	))
	ls = dv.style.MissingLine
	if l.after != nil {
		ls = dv.lineStyleForType(l.after.Kind)
	}
	dv.afterNumber(b, ls, "…")
	b.WriteString(full.after.Render(
		ls.Code.Width(dv.fullCodeWidth).Render("  …"),
	))
	b.WriteRune('\n')
}

// splitBefore writes the before half of a split row.
func (dv *DiffView) splitBefore(b *strings.Builder, fullContentStyle lipgloss.Style, l *udiff.Line, beforeLine int) {
	switch {
	case l == nil:
		ls := dv.style.MissingLine
		dv.beforeNumber(b, ls, " ")
		b.WriteString(fullContentStyle.Render(
			ls.Code.Width(dv.fullCodeWidth).Render("  "),
		))
	case l.Kind == udiff.Equal:
		ls := dv.style.EqualLine
		content, leadingEllipsis := dv.lineContent(l.Content, ls)
		dv.beforeNumber(b, ls, beforeLine)
		b.WriteString(fullContentStyle.Render(
			ls.Code.Width(dv.fullCodeWidth).Render(ternary(leadingEllipsis, " …", "  ") + content),
		))
	case l.Kind == udiff.Delete:
		ls := dv.style.DeleteLine
		content, leadingEllipsis := dv.lineContent(l.Content, ls)
		dv.beforeNumber(b, ls, beforeLine)
		b.WriteString(fullContentStyle.Render(
			ls.Symbol.Render(ternary(leadingEllipsis, "-…", "- ")) +
				ls.Code.Width(dv.codeWidth).Render(content),
		))
	}
}

// splitAfter writes the after half of a split row.
func (dv *DiffView) splitAfter(b *strings.Builder, fullContentStyle lipgloss.Style, l *udiff.Line, afterLine int) {
	switch {
	case l == nil:
		ls := dv.style.MissingLine
		dv.afterNumber(b, ls, " ")
		b.WriteString(fullContentStyle.Render(
			ls.Code.Width(dv.fullCodeWidth + btoi(dv.extraColOnAfter)).Render("  "),
		))
	case l.Kind == udiff.Equal:
		ls := dv.style.EqualLine
		content, leadingEllipsis := dv.lineContent(l.Content, ls)
		dv.afterNumber(b, ls, afterLine)
		b.WriteString(fullContentStyle.Render(
			ls.Code.Width(dv.fullCodeWidth + btoi(dv.extraColOnAfter)).Render(ternary(leadingEllipsis, " …", "  ") + content),
		))
	case l.Kind == udiff.Insert:
		ls := dv.style.InsertLine
		content, leadingEllipsis := dv.lineContent(l.Content, ls)
		dv.afterNumber(b, ls, afterLine)
		b.WriteString(fullContentStyle.Render(
			ls.Symbol.Render(ternary(leadingEllipsis, "+…", "+ ")) +
				ls.Code.Width(dv.codeWidth+btoi(dv.extraColOnAfter)).Render(content),
		))
	}
}

// hunkLineFor formats the header line for a hunk in the unified diff view.
func (dv *DiffView) hunkLineFor(h *udiff.Hunk) string {
	beforeShownLines, afterShownLines := dv.hunkShownLines(h)

	return fmt.Sprintf(
		"  @@ -%d,%d +%d,%d @@ ",
		h.FromLine,
		beforeShownLines,
		h.ToLine,
		afterShownLines,
	)
}

// hunkShownLines calculates the number of lines shown in a hunk for both before
// and after versions.
func (dv *DiffView) hunkShownLines(h *udiff.Hunk) (before, after int) {
	for _, l := range h.Lines {
		switch l.Kind {
		case udiff.Equal:
			before++
			after++
		case udiff.Insert:
			after++
		case udiff.Delete:
			before++
		}
	}
	return before, after
}

func (dv *DiffView) lineStyleForType(t udiff.OpKind) LineStyle {
	switch t {
	case udiff.Equal:
		return dv.style.EqualLine
	case udiff.Insert:
		return dv.style.InsertLine
	case udiff.Delete:
		return dv.style.DeleteLine
	default:
		return dv.style.MissingLine
	}
}

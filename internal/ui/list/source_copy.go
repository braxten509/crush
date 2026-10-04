package list

import (
	"image"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

// CopySource provides the original text for selection copying. Screen rows are
// presentation: neither a terminal wrap nor renderer padding belongs in code.
type CopySource interface {
	CopySource() []string
}

type sourceByte struct{ start, end int }
type copyCell struct {
	x, y, width int
	start, end  int
}

// HighlightSource maps selected visible characters back to the original message.
// Alignment ignores whitespace so added wraps/gutters cannot become spaces in a
// path or after a shell continuation. The returned slice retains the source's
// actual whitespace, including hard newlines. Unmatched UI text falls back to
// HighlightContent at the caller instead of inventing source text for it.
func HighlightSource(blocks []string, rendered string, area image.Rectangle, startLine, startCol, endLine, endCol int) (string, bool) {
	if len(blocks) == 0 || startLine < 0 || startCol < 0 {
		return "", false
	}
	if endLine < 0 {
		endLine = area.Dy() - 1
	}
	if endCol < 0 {
		endCol = area.Dx()
	}
	source := strings.Join(blocks, "\n\n")
	if strings.TrimSpace(source) == "" {
		// A blank code block has no glyph anchors. A whole-item selection still
		// has an exact source; don't silently turn its whitespace into nothing.
		whole := startLine == 0 && startCol == 0 && endLine >= area.Dy()-1 && endCol >= area.Dx() &&
			strings.TrimSpace(ansi.Strip(rendered)) == ""
		return source, whole
	}
	// Keep leading/trailing rows: trimming them shifts selection coordinates.
	rendered = strings.ReplaceAll(strings.ReplaceAll(rendered, "\r\n", "\n"), "\t", "    ")
	buf := renderBuffer(rendered, area, area.Dx(), area.Dy())
	var visible strings.Builder
	var cells []copyCell
	for y := 0; y < buf.Height(); y++ {
		line := buf.Line(y)
		for x := 0; x < len(line); x++ {
			cell := line.At(x)
			if cell == nil || cell.Content == "" {
				continue
			}
			text := cell.Content
			start := visible.Len()
			for _, r := range text {
				if !unicode.IsSpace(r) {
					visible.WriteRune(r)
				}
			}
			if visible.Len() > start {
				cells = append(cells, copyCell{x, y, max(1, cell.Width), start, visible.Len()})
			}
		}
	}

	var offsets []sourceByte
	for pos, r := range source {
		if unicode.IsSpace(r) {
			continue
		}
		size := utf8.RuneLen(r)
		for range size {
			offsets = append(offsets, sourceByte{pos, pos + size})
		}
	}
	// Match literal blocks separately: a long reply with hundreds of fences or
	// headings must not exhaust a whole-message diff budget and corrupt code.
	mapping := make([]int, visible.Len())
	for i := range mapping {
		mapping[i] = -1
	}
	visibleAt, sourceAt := 0, 0
	for _, block := range blocks {
		compact := strings.Map(func(r rune) rune {
			if unicode.IsSpace(r) {
				return -1
			}
			return r
		}, block)
		if compact == "" {
			continue
		}
		if at := strings.Index(visible.String()[visibleAt:], compact); at >= 0 {
			visibleAt += at
			for i := range len(compact) {
				mapping[visibleAt+i] = sourceAt + i
			}
			visibleAt += len(compact)
		}
		sourceAt += len(compact)
	}

	first, last := -1, -1
	for _, cell := range cells {
		if cell.y < startLine || cell.y > endLine ||
			cell.y == startLine && cell.x+cell.width <= startCol ||
			cell.y == endLine && cell.x >= endCol {
			continue
		}
		for i := cell.start; i < cell.end; i++ {
			if mapping[i] < 0 {
				return "", false
			}
		}
		if first < 0 {
			first = offsets[mapping[cell.start]].start
		}
		last = offsets[mapping[cell.end-1]].end
	}
	// Map source whitespace only where it has visible cells; screen padding is
	// never a source. Whitespace inside the selected span is already preserved.
	for _, span := range whitespaceCells(source, cells, mapping, offsets, area.Dx()) {
		if selectedCell(span.cell, startLine, startCol, endLine, endCol) {
			if first < 0 || span.source.start < first {
				first = span.source.start
			}
			if span.source.end > last {
				last = span.source.end
			}
		}
	}
	if first < 0 {
		return "", true
	}
	return source[first:last], true
}

func selectedCell(cell copyCell, startLine, startCol, endLine, endCol int) bool {
	if cell.width == 0 { // A source newline crosses a row boundary.
		return cell.y >= startLine && cell.y < endLine
	}
	return cell.y >= startLine && cell.y <= endLine &&
		(cell.y != startLine || cell.x+cell.width > startCol) &&
		(cell.y != endLine || cell.x < endCol)
}

type whitespaceCell struct {
	cell   copyCell
	source sourceByte
}

// whitespaceCells uses adjacent source characters as anchors. Spaces between
// words map directly, indentation ends at the first visible glyph, and actual
// newlines cross rows. Soft wrapping adds no source characters.
func whitespaceCells(source string, cells []copyCell, mapping []int, offsets []sourceByte, width int) []whitespaceCell {
	type anchor struct {
		cell       copyCell
		start, end int
	}
	var anchors []anchor
	for _, cell := range cells {
		if mapping[cell.start] >= 0 && mapping[cell.end-1] >= 0 {
			anchors = append(anchors, anchor{cell, offsets[mapping[cell.start]].start, offsets[mapping[cell.end-1]].end})
		}
	}
	if len(anchors) == 0 {
		return nil
	}
	var result []whitespaceCell
	for index := 0; index <= len(anchors); index++ {
		from, to := 0, len(source)
		var before, after *anchor
		if index > 0 {
			before = &anchors[index-1]
			from = before.end
		}
		if index < len(anchors) {
			after = &anchors[index]
			to = after.start
		}
		if from >= to || strings.TrimSpace(source[from:to]) != "" {
			continue
		}
		gap := source[from:to]
		newlines := strings.Count(gap, "\n")
		if before != nil && after != nil && newlines == 0 && before.cell.y != after.cell.y {
			continue // whitespace consumed by wrapping is not a selectable cell
		}
		x, y := 0, 0
		if before != nil {
			x, y = before.cell.x+before.cell.width, before.cell.y
		}
		if before == nil && after != nil {
			y = after.cell.y - newlines
		}
		// The final indentation run is right-aligned against its source glyph,
		// which excludes any renderer gutter on that row.
		lastBreak := strings.LastIndexByte(gap, '\n')
		indentWidth := ansi.StringWidth(strings.ReplaceAll(gap[lastBreak+1:], "\t", "    "))
		gutter := 0
		if after != nil {
			gutter = max(0, after.cell.x-indentWidth)
		}
		if before != nil {
			lineStart := strings.LastIndexByte(source[:before.start], '\n') + 1
			// A wrapped line's last glyph no longer has its source column on
			// screen. Its first glyph still identifies the content gutter.
			first := anchors[sort.Search(index, func(i int) bool { return anchors[i].start >= lineStart })]
			column := ansi.StringWidth(strings.ReplaceAll(source[lineStart:first.start], "\t", "    "))
			if first.cell.x >= column {
				gutter = first.cell.x - column
			}
		} else {
			x = gutter
		}
		for offset, r := range gap {
			if after != nil && offset == lastBreak+1 {
				x = after.cell.x - indentWidth
				y = after.cell.y
			}
			size := utf8.RuneLen(r)
			cell := copyCell{x: x, y: y, width: 1}
			if r == '\n' {
				cell.x = width
				cell.width = 0
				y++
				x = gutter
			} else {
				if r == '\t' {
					cell.width = 4
				}
				x += cell.width
			}
			result = append(result, whitespaceCell{cell, sourceByte{from + offset, from + offset + size}})
		}
	}
	return result
}

package questionform

import (
	"fmt"
	"image"
	"strings"

	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

// Tab bar geometry.
const (
	// tabPadX is the horizontal padding inside a tab, on each side.
	tabPadX = 1
	// tabHeight is the height of a tab, borders included.
	tabHeight = 3
	// tabMinLabelW is the narrowest a truncated tab label gets.
	tabMinLabelW = 1
)

// drawTabBar draws the tabs along the top of area, records their hit
// layers for mouse clicks, and returns the row where the tab content
// starts.
func (f *QuestionForm) drawTabBar(scr uv.Screen, area uv.Rectangle) int {
	labels, tabWidths := f.tabLabels(area.Dx())

	// Build tab layers for click hit detection.
	var layers []*lipgloss.Layer
	x := area.Min.X

	hoveredTab := f.hoveredTab(area, tabWidths)

	firstVisible := -1
	for i := range labels {
		if tabWidths[i] > 0 {
			firstVisible = i
			break
		}
	}

	for i, label := range labels {
		// Skip hidden tabs (single-tab mode).
		if tabWidths[i] == 0 {
			continue
		}
		tabWidth := tabWidths[i]

		tabArea := image.Rect(x, area.Min.Y, x+tabWidth, area.Min.Y+tabHeight)
		f.drawTab(scr, tabArea, i, label, i == hoveredTab, i == firstVisible)

		// Create an invisible hit layer for this tab.
		hitStr := strings.Repeat(strings.Repeat(" ", tabWidth)+"\n", tabHeight-1) + strings.Repeat(" ", tabWidth)
		layers = append(layers, lipgloss.NewLayer(hitStr).X(x).Y(area.Min.Y).ID(fmt.Sprintf("tab_%d", i)))

		x += tabWidth
	}

	f.compositor = lipgloss.NewCompositor(layers...)

	lineY := area.Min.Y + tabHeight - 1
	lineSide := f.Styles.Tab.InactiveBorder.Bottom
	if !f.focused {
		lineSide = f.Styles.Tab.InactiveBorderBlurred.Bottom
	}
	for lx := x; lx < area.Max.X; lx++ {
		c := uv.NewCell(scr.WidthMethod(), lineSide.Content)
		if c != nil {
			c.Style = lineSide.Style
		}
		scr.SetCell(lx, lineY, c)
	}

	return area.Min.Y + tabHeight + 1
}

// tabLabels returns the tab labels and widths that fit in avail cells.
// A zero width hides a tab.
func (f *QuestionForm) tabLabels(avail int) ([]string, []int) {
	// Compute display labels.
	labels := make([]string, len(f.labels))
	copy(labels, f.labels)

	// Truncate if tabs exceed width. Distribute the available
	// space fairly: short labels keep their natural width and
	// the deficit is shared proportionally among longer ones,
	// with remainder cells distributed left-to-right so the
	// layout resizes smoothly pixel-by-pixel.
	tabWidths := make([]int, len(labels))
	naturalWidths := make([]int, len(labels))
	totalWidth := 0
	for i, l := range labels {
		w := ansi.StringWidth(l) + tabPadX*2 + 2
		tabWidths[i] = w
		naturalWidths[i] = w
		totalWidth += w
	}
	if totalWidth <= avail || len(labels) == 0 {
		return labels, tabWidths
	}

	// Check if there's enough room to show all tabs with
	// at least a useful label. If each tab can't fit at
	// least 5 cells of label, switch to single-tab mode
	// with a "N of M" counter.
	usefulMinTabW := 5 + tabPadX*2 + 2
	if avail/len(labels) < usefulMinTabW {
		f.singleTabLabels(labels, tabWidths, avail)
		return labels, tabWidths
	}

	// Normal truncation: distribute space fairly.
	fairTabWidths(tabWidths, naturalWidths, avail)

	// Apply truncation based on final widths.
	for i, l := range labels {
		labelAvail := max(tabWidths[i]-tabPadX*2-2, tabMinLabelW)
		if ansi.StringWidth(l) > labelAvail {
			labels[i] = ansi.Truncate(l, labelAvail, "…")
		}
	}
	return labels, tabWidths
}

// singleTabLabels shows only the active tab, its label followed by an
// "N/M" counter, and hides the others.
func (f *QuestionForm) singleTabLabels(labels []string, tabWidths []int, avail int) {
	n := len(labels)
	counter := fmt.Sprintf("%d/%d", f.activeIdx+1, n)
	activeLabel := labels[f.activeIdx]
	combined := activeLabel + " · " + counter
	maxLabel := avail - tabPadX*2 - 2
	if maxLabel < 3 {
		maxLabel = 3
	}
	if ansi.StringWidth(combined) > maxLabel {
		// Truncate the label part to fit.
		counterPart := " · " + counter
		labelBudget := maxLabel - ansi.StringWidth(counterPart)
		if labelBudget < 1 {
			labelBudget = 1
		}
		combined = ansi.Truncate(activeLabel, labelBudget, "…") + counterPart
	}
	for i := range labels {
		if i == f.activeIdx {
			labels[i] = combined
		} else {
			labels[i] = ""
		}
	}
	// Recalculate widths for single visible tab.
	for i := range labels {
		if labels[i] == "" {
			tabWidths[i] = 0
		} else {
			tabWidths[i] = ansi.StringWidth(labels[i]) + tabPadX*2 + 2
		}
	}
}

// fairTabWidths shrinks tabWidths to fit avail: tabs narrower than an
// even share keep their natural width, and the rest share what is left,
// with remainder cells handed out left to right.
func fairTabWidths(tabWidths, naturalWidths []int, avail int) {
	minTabW := tabMinLabelW + tabPadX*2 + 2
	n := len(tabWidths)
	capped := make([]bool, n)
	for {
		freeCount := 0
		freeTotal := 0
		for i := range n {
			if capped[i] {
				continue
			}
			freeCount++
			freeTotal += naturalWidths[i]
		}
		if freeCount == 0 {
			break
		}
		budget := avail
		for i := range n {
			if capped[i] {
				budget -= tabWidths[i]
			}
		}
		share := budget / freeCount
		changed := false
		for i := range n {
			if !capped[i] && naturalWidths[i] <= share {
				capped[i] = true
				tabWidths[i] = naturalWidths[i]
				changed = true
			}
		}
		if !changed {
			for i := range n {
				if !capped[i] {
					tabWidths[i] = max(share, minTabW)
				}
			}
			remainder := budget - share*freeCount
			for i := range n {
				if remainder <= 0 {
					break
				}
				if !capped[i] && tabWidths[i] < naturalWidths[i] {
					tabWidths[i]++
					remainder--
				}
			}
			break
		}
	}
}

// hoveredTab returns the index of the tab under the mouse, or -1.
func (f *QuestionForm) hoveredTab(area uv.Rectangle, tabWidths []int) int {
	if f.hoverY < area.Min.Y || f.hoverY >= area.Min.Y+tabHeight {
		return -1
	}
	tx := area.Min.X
	for i, tw := range tabWidths {
		if f.hoverX >= tx && f.hoverX < tx+tw {
			return i
		}
		tx += tw
	}
	return -1
}

// drawTab draws tab i with its border and centered label in tabArea.
func (f *QuestionForm) drawTab(scr uv.Screen, tabArea image.Rectangle, i int, label string, underMouse, firstVisible bool) {
	isActive := i == f.activeIdx
	isHovered := underMouse && !isActive
	labelWidth := ansi.StringWidth(label)
	tabWidth := tabArea.Dx()

	border := f.Styles.Tab.InactiveBorder
	textStyle := f.Styles.Tab.InactiveStyle
	if !f.focused {
		border = f.Styles.Tab.InactiveBorderBlurred
	}
	if isActive {
		border = f.Styles.Tab.ActiveBorder
		textStyle = f.Styles.Tab.ActiveStyle
		if !f.focused {
			border = f.Styles.Tab.ActiveBorderBlurred
		}
	} else if i < f.numQuestions && f.isAnswered(i) {
		textStyle = f.Styles.Tab.ActiveStyle
	}
	if isHovered {
		hovered := textStyle
		hovered.Attrs |= uv.AttrBold
		textStyle = hovered
	}

	if firstVisible {
		if isActive {
			border.BottomLeft = uv.Side{Content: "┘", Style: border.BottomLeft.Style}
		} else {
			border.BottomLeft = uv.Side{Content: "┴", Style: border.BottomLeft.Style}
		}
	}

	border.Draw(scr, tabArea)

	innerWidth := tabWidth - 2
	xOff := (innerWidth - labelWidth) / 2
	innerArea := image.Rect(
		tabArea.Min.X+1+xOff, tabArea.Min.Y+1,
		tabArea.Max.X-1, tabArea.Max.Y-1,
	)
	uv.NewStyledString(textStyle.Styled(label)).Draw(scr, innerArea)
}

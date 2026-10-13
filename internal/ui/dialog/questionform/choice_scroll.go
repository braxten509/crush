package questionform

// clampScroll keeps the cursor item visible using a sliding
// window: the cursor moves freely within the visible region and
// only pushes the window when it reaches an edge. Going down pushes
// the bottom; going up pushes the top until the start (header and
// HandleWheel scrolls the choice list vertically. Satisfies
// WheelScrollableEditor so the form can delegate wheel events.
func (c *choiceList) HandleWheel(deltaX, deltaY float64) {
	if deltaY == 0 {
		return
	}
	c.scrollOffset += int(deltaY)
	c.wheelActive = true
	c.clampToBounds(c.lastLines, c.lastViewport)
}

// clampToBounds enforces scroll bounds [0, max] and keeps the
// fill-in at least partially visible when active. It does NOT
// snap to the cursor, making it safe for wheel-driven scrolling.
func (c *choiceList) clampToBounds(lines []contentLine, viewport int) {
	limit := max(0, len(lines)-viewport)
	c.scrollOffset = min(max(0, c.scrollOffset), limit)
	// When fill-in is active, ensure at least one fill-in line
	// remains visible in the viewport.
	if c.isFillIn() && c.fillInTop >= 0 {
		fillInBottom := c.fillInBottom
		if fillInBottom < 0 {
			fillInBottom = c.fillInTop
		}
		// If scrolled past the fill-in entirely, pull back so
		// the last fill-in line is at the bottom of the viewport.
		if c.scrollOffset > fillInBottom {
			c.scrollOffset = max(0, fillInBottom-viewport+1)
		}
		// If scrolled above the fill-in start while fill-in is
		// taller than the viewport, pin to fill-in start.
		if c.scrollOffset+viewport <= c.fillInTop && fillInBottom-c.fillInTop+1 >= viewport {
			c.scrollOffset = c.fillInTop
		}
	}
}

// clampScroll keeps the cursor item visible using a sliding
// window: the cursor moves freely within the visible region and
// only pushes the window when it reaches an edge. Going down pushes
// the bottom; going up pushes the top until the start (header and
// description) comes back into view.
func (c *choiceList) clampScroll(lines []contentLine, viewport int) {
	if c.suppressScroll {
		c.suppressScroll = false
		return
	}
	// After wheel scroll, only enforce bounds instead of snapping
	// to cursor. Wheel mode persists until the next keyboard nav.
	if c.wheelActive {
		c.clampToBounds(lines, viewport)
		return
	}
	limit := max(0, len(lines)-viewport)
	if limit == 0 {
		c.scrollOffset = 0
		return
	}

	// Row range of the cursor item.
	cursorTop, cursorBottom := -1, -1
	for i, ln := range lines {
		if ln.cursorItem {
			if cursorTop < 0 {
				cursorTop = i
			}
			cursorBottom = i
		}
	}
	// When fill-in is focused, narrow the cursor target to the
	// specific textarea cursor line so typing auto-scrolls.
	if c.isFillIn() && c.fillIn.Focused() && cursorTop >= 0 {
		if tc := c.fillIn.Cursor(); tc != nil {
			targetLine := cursorTop + tc.Y
			if targetLine >= cursorTop && targetLine <= cursorBottom {
				cursorTop = targetLine
				cursorBottom = targetLine
			}
		}
	}
	if cursorTop < 0 {
		c.scrollOffset = min(max(0, c.scrollOffset), limit)
		return
	}

	// Keep one line below the cursor visible (trailing pad on the
	// last item, a separator otherwise) so the selection is never
	// flush against the bottom edge.
	below := min(cursorBottom+1, len(lines)-1)

	// On the first selectable item, prefer the top so the header
	// and description (nothing selectable sits above them) come
	// into view.
	if c.cursorIdx == 0 {
		c.scrollOffset = 0
	}
	// Push the window down if the cursor's bottom fell below it.
	if below >= c.scrollOffset+viewport {
		c.scrollOffset = below - viewport + 1
	}
	// Push the window up if the cursor's top rose above it.
	if cursorTop < c.scrollOffset {
		c.scrollOffset = cursorTop
	}

	c.scrollOffset = min(max(0, c.scrollOffset), limit)
}

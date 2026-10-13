package list

// SelectedItemInView returns whether the selected item is currently in view.
func (l *List) SelectedItemInView() bool {
	if l.selectedIdx < 0 || l.selectedIdx >= len(l.items) {
		return false
	}
	startIdx, endIdx := l.VisibleItemIndices()
	return l.selectedIdx >= startIdx && l.selectedIdx <= endIdx
}

// SetSelected sets the selected item index in the list.
// It returns -1 if the index is out of bounds.
func (l *List) SetSelected(index int) {
	if index < 0 || index >= len(l.items) {
		l.selectedIdx = -1
	} else {
		l.selectedIdx = index
	}
}

// Selected returns the index of the currently selected item. It returns -1 if
// no item is selected.
func (l *List) Selected() int {
	return l.selectedIdx
}

// IsSelectedFirst returns whether the first item is selected.
func (l *List) IsSelectedFirst() bool {
	return l.selectedIdx == 0
}

// IsSelectedLast returns whether the last item is selected.
func (l *List) IsSelectedLast() bool {
	return l.selectedIdx == len(l.items)-1
}

// SelectPrev selects the visually previous item (moves toward visual top).
// It returns whether the selection changed.
func (l *List) SelectPrev() bool {
	if l.reverse {
		// In reverse, visual up = higher index
		if l.selectedIdx < len(l.items)-1 {
			l.selectedIdx++
			return true
		}
	} else {
		// Normal: visual up = lower index
		if l.selectedIdx > 0 {
			l.selectedIdx--
			return true
		}
	}
	return false
}

// SelectNext selects the next item in the list.
// It returns whether the selection changed.
func (l *List) SelectNext() bool {
	if l.reverse {
		// In reverse, visual down = lower index
		if l.selectedIdx > 0 {
			l.selectedIdx--
			return true
		}
	} else {
		// Normal: visual down = higher index
		if l.selectedIdx < len(l.items)-1 {
			l.selectedIdx++
			return true
		}
	}
	return false
}

// SelectFirst selects the first item in the list.
// It returns whether the selection changed.
func (l *List) SelectFirst() bool {
	if len(l.items) == 0 {
		return false
	}
	l.selectedIdx = 0
	return true
}

// SelectLast selects the last item in the list (highest index).
// It returns whether the selection changed.
func (l *List) SelectLast() bool {
	if len(l.items) == 0 {
		return false
	}
	l.selectedIdx = len(l.items) - 1
	return true
}

// WrapToStart wraps selection to the visual start (for circular navigation).
// In normal mode, this is index 0. In reverse mode, this is the highest index.
func (l *List) WrapToStart() bool {
	if len(l.items) == 0 {
		return false
	}
	if l.reverse {
		l.selectedIdx = len(l.items) - 1
	} else {
		l.selectedIdx = 0
	}
	return true
}

// WrapToEnd wraps selection to the visual end (for circular navigation).
// In normal mode, this is the highest index. In reverse mode, this is index 0.
func (l *List) WrapToEnd() bool {
	if len(l.items) == 0 {
		return false
	}
	if l.reverse {
		l.selectedIdx = 0
	} else {
		l.selectedIdx = len(l.items) - 1
	}
	return true
}

// SelectedItem returns the currently selected item. It may be nil if no item
// is selected.
func (l *List) SelectedItem() Item {
	if l.selectedIdx < 0 || l.selectedIdx >= len(l.items) {
		return nil
	}
	return l.items[l.selectedIdx]
}

// SelectFirstInView selects the first item currently in view.
func (l *List) SelectFirstInView() {
	startIdx, _ := l.VisibleItemIndices()
	l.selectedIdx = startIdx
}

// SelectLastInView selects the last item currently in view.
func (l *List) SelectLastInView() {
	_, endIdx := l.VisibleItemIndices()
	l.selectedIdx = endIdx
}

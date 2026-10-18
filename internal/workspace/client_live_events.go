package workspace

import tea "charm.land/bubbletea/v2"

// translateLiveEvent translates piece B's events (live work, ownership and
// the smaller gaps, engine-design.md 5.3, 5.4, 5.5 and 5.9). It reports
// false for an event that is not B's.
func (w *ClientWorkspace) translateLiveEvent(any) (tea.Msg, bool) { return nil, false }

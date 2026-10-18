package workspace

import tea "charm.land/bubbletea/v2"

// translateShareEvent translates piece D's events (phone sharing and secure
// entry, engine-design.md 5.7 and 5.8). It reports false for an event that
// is not D's.
func (w *ClientWorkspace) translateShareEvent(any) (tea.Msg, bool) { return nil, false }

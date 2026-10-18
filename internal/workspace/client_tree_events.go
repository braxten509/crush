package workspace

import tea "charm.land/bubbletea/v2"

// translateTreeEvent translates piece C's events (tree actions and branch
// copy, engine-design.md 5.6 and 5.11). It reports false for an event that
// is not C's.
func (w *ClientWorkspace) translateTreeEvent(any) (tea.Msg, bool) { return nil, false }

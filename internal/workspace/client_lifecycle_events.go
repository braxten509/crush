package workspace

import tea "charm.land/bubbletea/v2"

// translateLifecycleEvent translates piece A's events (engine lifecycle and
// wire, engine-design.md 5.1, 5.2, 5.10 and 5.14). It reports false for an
// event that is not A's.
func (w *ClientWorkspace) translateLifecycleEvent(any) (tea.Msg, bool) { return nil, false }

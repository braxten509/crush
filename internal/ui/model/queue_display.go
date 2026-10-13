package model

import (
	"github.com/charmbracelet/crush/internal/ui/chat"
)

// A submitted message has one visible home. Delivery can still be pending
// internally, but never duplicate a timeline message in the queue panel.
func (m *UI) visiblePromptQueueItems() []string {
	visibleIDs := make(map[string]bool)
	for _, item := range m.chat.flat {
		if user, ok := item.(*chat.UserMessageItem); ok && user.SubmissionID() != "" {
			visibleIDs[user.SubmissionID()] = true
		}
	}
	var result []string
	// Legacy summaries have no IDs. Match their pending timeline copies once.
	copies := make(map[string]int)
	for _, pending := range m.queue.pending {
		if pending.SessionID == m.currentSessionID() && !m.queue.held[pending.ID] {
			copies[pending.Content().Text]++
		}
	}
	for id, confirmed := range m.queue.confirmedCopies {
		if confirmed.SessionID == m.currentSessionID() && visibleIDs[id] {
			copies[confirmed.Content().Text]++
		}
	}
	if len(m.queue.entries) == len(m.queue.items) && len(m.queue.entries) > 0 {
		for _, entry := range m.queue.entries {
			if entry.SubmissionID != "" && visibleIDs[entry.SubmissionID] && copies[entry.Prompt] > 0 {
				copies[entry.Prompt]--
			}
		}
		for _, entry := range m.queue.entries {
			if entry.SubmissionID == "" && copies[entry.Prompt] > 0 {
				copies[entry.Prompt]--
				continue
			}
			if !visibleIDs[entry.SubmissionID] {
				result = append(result, entry.Prompt)
			}
		}
		return append(result, m.queue.unlistedHeld(m.currentSessionID())...)
	}
	// Older workspaces only expose strings; match each pending copy once so
	// two identical messages still remain two distinct submissions.
	for _, prompt := range m.queue.items {
		if copies[prompt] > 0 {
			copies[prompt]--
			continue
		}
		result = append(result, prompt)
	}
	if len(m.queue.items) == 0 {
		result = append(result, m.queue.unlistedHeld(m.currentSessionID())...)
	}
	return result
}

func (m *UI) visiblePromptQueueCount() int {
	if m.queue.items == nil && len(m.queue.held) == 0 {
		return m.queue.count
	}
	return len(m.visiblePromptQueueItems())
}

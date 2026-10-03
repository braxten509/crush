package model

import (
	"slices"

	"github.com/charmbracelet/crush/internal/message"
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
	for _, pending := range m.pendingPrompts {
		if pending.SessionID == m.currentSessionID() && !m.heldPrompts[pending.ID] {
			copies[pending.Content().Text]++
		}
	}
	for id, confirmed := range m.confirmedQueueCopies {
		if confirmed.SessionID == m.currentSessionID() && visibleIDs[id] {
			copies[confirmed.Content().Text]++
		}
	}
	if len(m.promptQueueEntries) == len(m.promptQueueItems) && len(m.promptQueueEntries) > 0 {
		for _, entry := range m.promptQueueEntries {
			if entry.SubmissionID != "" && visibleIDs[entry.SubmissionID] && copies[entry.Prompt] > 0 {
				copies[entry.Prompt]--
			}
		}
		for _, entry := range m.promptQueueEntries {
			if entry.SubmissionID == "" && copies[entry.Prompt] > 0 {
				copies[entry.Prompt]--
				continue
			}
			if !visibleIDs[entry.SubmissionID] {
				result = append(result, entry.Prompt)
			}
		}
		return append(result, m.unlistedHeldPrompts()...)
	}
	// Older workspaces only expose strings; match each pending copy once so
	// two identical messages still remain two distinct submissions.
	for _, prompt := range m.promptQueueItems {
		if copies[prompt] > 0 {
			copies[prompt]--
			continue
		}
		result = append(result, prompt)
	}
	if len(m.promptQueueItems) == 0 {
		result = append(result, m.unlistedHeldPrompts()...)
	}
	return result
}

// unlistedHeldPrompts are held prompts the agent's queue does not list yet,
// so a prompt shows in the queue list from the moment it is sent.
func (m *UI) unlistedHeldPrompts() []string {
	var result []string
	for _, pending := range m.pendingPrompts {
		if pending.SessionID != m.currentSessionID() || !m.heldPrompts[pending.ID] {
			continue
		}
		if !slices.ContainsFunc(m.promptQueueEntries, func(e message.QueuedPromptSummary) bool { return e.SubmissionID == pending.ID }) {
			result = append(result, pending.Content().Text)
		}
	}
	return result
}

func (m *UI) visiblePromptQueueCount() int {
	if m.promptQueueItems == nil && len(m.heldPrompts) == 0 {
		return m.promptQueue
	}
	return len(m.visiblePromptQueueItems())
}

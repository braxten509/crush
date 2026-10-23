package agent

import "github.com/charmbracelet/crush/internal/message"

// queuedPromptSnapshots is every chat's prompts waiting behind its turn, as
// this agent's scheduler sees them: those handed to an agent CLI but not
// taken in yet, those an interrupt will run, and those in the queue.
func (s *sessionScheduler) queuedPromptSnapshots() map[string][]message.QueuedPrompt {
	out := map[string][]message.QueuedPrompt{}
	add := func(sessionID string, p message.QueuedPrompt) {
		out[sessionID] = append(out[sessionID], p)
	}
	for sessionID, steered := range s.steering.Seq2() {
		for _, text := range steered.pendingSteered() {
			add(sessionID, message.QueuedPrompt{Prompt: text})
		}
	}
	for sessionID, pending := range s.interrupting.Seq2() {
		for _, call := range pending {
			add(sessionID, message.QueuedPrompt{Prompt: call.Prompt, Attachments: call.Attachments, SubmissionID: call.SubmissionID})
		}
	}
	for sessionID, queued := range s.messageQueue.Seq2() {
		for _, call := range queued {
			add(sessionID, message.QueuedPrompt{Prompt: call.Prompt, Attachments: call.Attachments, SubmissionID: call.SubmissionID})
		}
	}
	return out
}

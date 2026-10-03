package agent

import "github.com/charmbracelet/crush/internal/message"

func (s *cliSteps) pendingSteeredSummaries() []message.QueuedPromptSummary {
	s.steerMu.Lock()
	defer s.steerMu.Unlock()
	var result []message.QueuedPromptSummary
	for _, st := range s.steered {
		if st.hidden {
			continue
		}
		for _, call := range st.calls {
			result = append(result, message.QueuedPromptSummary{Prompt: call.Prompt, SubmissionID: call.SubmissionID})
		}
	}
	return result
}

func (a *sessionAgent) QueuedPromptSummaries(sessionID string) []message.QueuedPromptSummary {
	var result []message.QueuedPromptSummary
	if s, _ := a.steering.Get(sessionID); s != nil {
		result = s.pendingSteeredSummaries()
	}
	pending, _ := a.interrupting.Get(sessionID)
	queued, _ := a.messageQueue.Get(sessionID)
	for _, calls := range [][]SessionAgentCall{pending, queued} {
		for _, call := range calls {
			result = append(result, message.QueuedPromptSummary{Prompt: call.Prompt, SubmissionID: call.SubmissionID})
		}
	}
	return result
}

// QueuedPromptSummaries retains compatibility with coordinators and agents
// implementing the older string-only queue API.
func QueueSummaries(source interface{ QueuedPromptsList(string) []string }, sessionID string) []message.QueuedPromptSummary {
	if source == nil {
		return nil
	}
	if detailed, ok := source.(interface {
		QueuedPromptSummaries(string) []message.QueuedPromptSummary
	}); ok {
		return detailed.QueuedPromptSummaries(sessionID)
	}
	var result []message.QueuedPromptSummary
	for _, prompt := range source.QueuedPromptsList(sessionID) {
		result = append(result, message.QueuedPromptSummary{Prompt: prompt})
	}
	return result
}

func (c *coordinator) QueuedPromptSummaries(sessionID string) []message.QueuedPromptSummary {
	return QueueSummaries(c.currentAgent(), sessionID)
}

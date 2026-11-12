package agent

import "github.com/charmbracelet/crush/internal/message"

// pendingSteeredCalls lists what pendingSteered lists, each prompt with the
// files sent along.
func (s *cliSteps) pendingSteeredCalls() []message.QueuedPrompt {
	s.steerMu.Lock()
	defer s.steerMu.Unlock()
	var prompts []message.QueuedPrompt
	for _, st := range s.steered {
		for _, q := range st.calls {
			if !q.ProgressUpdateRequired {
				prompts = append(prompts, message.QueuedPrompt{Prompt: q.Prompt, Attachments: q.Attachments, SubmissionID: q.SubmissionID})
			}
		}
	}
	return prompts
}

// QueuedPromptFiles lists the prompts QueuedPromptsList lists, in the same
// order, each with the files sent along (the phone shows them as chips on the
// waiting message). The files are not copied.
func (s *sessionScheduler) QueuedPromptFiles(sessionID string) []message.QueuedPrompt {
	var prompts []message.QueuedPrompt
	if s, _ := s.steering.Get(sessionID); s != nil {
		prompts = s.pendingSteeredCalls()
	}
	pending, _ := s.interrupting.Get(sessionID)
	queued, _ := s.messageQueue.Get(sessionID)
	for _, calls := range [][]SessionAgentCall{pending, queued} {
		for _, call := range calls {
			if !call.ProgressUpdateRequired {
				prompts = append(prompts, message.QueuedPrompt{Prompt: call.Prompt, Attachments: call.Attachments, SubmissionID: call.SubmissionID})
			}
		}
	}
	return prompts
}

// QueueFiles is QueuedPromptFiles of a coordinator or agent that has it;
// one with only the text list gives the prompts without files.
func QueueFiles(source interface{ QueuedPromptsList(string) []string }, sessionID string) []message.QueuedPrompt {
	if source == nil {
		return nil
	}
	if detailed, ok := source.(interface {
		QueuedPromptFiles(string) []message.QueuedPrompt
	}); ok {
		return detailed.QueuedPromptFiles(sessionID)
	}
	var result []message.QueuedPrompt
	for _, prompt := range source.QueuedPromptsList(sessionID) {
		result = append(result, message.QueuedPrompt{Prompt: prompt})
	}
	return result
}

func (c *coordinator) QueuedPromptFiles(sessionID string) []message.QueuedPrompt {
	return QueueFiles(c.currentAgent(), sessionID)
}

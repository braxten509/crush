package model

import "github.com/charmbracelet/crush/internal/message"

type agentInterruptedMsg struct{ sessionID string }

// Buffered stream events must not make an interrupted action look active again.
func (m *UI) applyImmediateCancel(msg message.Message) message.Message {
	return withImmediateCancel(m.canceledMessages, msg)
}

// withImmediateCancel is applyImmediateCancel against a given set, so a
// session load can apply a snapshot of it off the UI thread.
func withImmediateCancel(canceledMessages map[string]struct{}, msg message.Message) message.Message {
	if _, canceled := canceledMessages[msg.ID]; !canceled {
		return msg
	}
	if msg.IsFinished() {
		return msg
	}
	msg = msg.Clone()
	msg.AddFinish(message.FinishReasonCanceled, "", "")
	return msg
}

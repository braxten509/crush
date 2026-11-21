package message

import "context"

// CodexCreditStore saves estimated Codex usage with the chat's stored messages.
type CodexCreditStore interface {
	RecordCodexCredits(ctx context.Context, sessionID, threadID, turnID string, credits float64) error
	CodexCreditsUsed(ctx context.Context, sessionID string) (float64, error)
}

// CodexCredits returns the optional credit store for this message service.
func (s *service) CodexCredits() (CodexCreditStore, bool) {
	store, ok := s.q.(CodexCreditStore)
	return store, ok
}

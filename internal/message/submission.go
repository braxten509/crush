package message

import "context"

type submissionIDKey struct{}

// DefinitiveSubmissionError means the call ended or was rejected, so a
// missing persisted message can safely be restored as a draft.
type DefinitiveSubmissionError struct {
	Err error
}

func (e *DefinitiveSubmissionError) Error() string { return e.Err.Error() }
func (e *DefinitiveSubmissionError) Unwrap() error { return e.Err }

// WithSubmissionID correlates immediate UI feedback with the saved user message.
func WithSubmissionID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, submissionIDKey{}, id)
}

func SubmissionID(ctx context.Context) string {
	id, _ := ctx.Value(submissionIDKey{}).(string)
	return id
}

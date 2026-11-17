package message

import "context"

// turnIDKey carries the id of the agent turn a context belongs to.
type turnIDKey struct{}

// WithTurnID tags ctx with the agent turn that runs under it, so messages
// created with it carry that turn's id (Message.TurnID).
func WithTurnID(ctx context.Context, id string) context.Context {
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, turnIDKey{}, id)
}

// TurnIDFrom returns the turn id set by WithTurnID, or "".
func TurnIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(turnIDKey{}).(string)
	return id
}

package providers

import (
	"context"

	"charm.land/fantasy"
)

// An empty stream with no finish reason is an interrupted response, not a
// completed turn. Use the normal bounded retry policy, including its wait
// and cancellation handling, without replaying completed tools.
type emptyStreamModel struct {
	fantasy.LanguageModel
}

func (m emptyStreamModel) Stream(ctx context.Context, call fantasy.Call) (fantasy.StreamResponse, error) {
	stream, err := m.LanguageModel.Stream(ctx, call)
	if err != nil {
		return nil, err
	}
	return func(yield func(fantasy.StreamPart) bool) {
		content, stopped := false, false
		reason := fantasy.FinishReasonUnknown
		stream(func(part fantasy.StreamPart) bool {
			switch part.Type {
			case fantasy.StreamPartTypeFinish:
				reason = part.FinishReason
			case fantasy.StreamPartTypeKeepalive, fantasy.StreamPartTypeWarnings:
			default:
				content = true
			}
			stopped = !yield(part)
			return !stopped
		})
		if stopped || content || (reason != "" && reason != fantasy.FinishReasonUnknown) {
			return
		}
		if err := ctx.Err(); err != nil {
			yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeError, Error: err})
			return
		}
		yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeError, Error: fantasy.NewIncompleteStreamError()})
	}, nil
}

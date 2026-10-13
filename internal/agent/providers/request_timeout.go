package providers

import (
	"context"
	"errors"
	"fmt"
	"time"

	"charm.land/fantasy"
)

// RequestTimeoutError reports that an LLM request exhausted its configured
// request_timeout budget. For streaming requests this is an idle timeout:
// it only fires when the provider sends nothing for the whole window, so a
// slow but actively streaming response is never killed. It wraps the
// underlying error so callers can still match [context.DeadlineExceeded]
// through the chain.
type RequestTimeoutError struct {
	// Timeout is the budget the request exhausted.
	Timeout time.Duration
	// Idle reports a stream that went silent rather than a request that
	// never answered.
	Idle bool
	// Cause is the error the deadline produced.
	Cause error
}

func (e *RequestTimeoutError) Error() string {
	msg := fmt.Sprintf("LLM request timed out after %s", e.Timeout)
	if e.Idle {
		msg = fmt.Sprintf("LLM stream received no data for %s", e.Timeout)
	}
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", msg, e.Cause)
	}
	return msg
}

func (e *RequestTimeoutError) Unwrap() error { return e.Cause }

// UserMessage explains the timeout in the UI, including how long the request
// ran before giving up and how to change the limit.
func (e *RequestTimeoutError) UserMessage() string {
	hint := "Increase the limit with \"option request-timeout SECONDS\" or set it to 0 to disable the timeout."
	if e.Idle {
		return fmt.Sprintf("The model stopped sending data for %s. %s", e.Timeout, hint)
	}
	return fmt.Sprintf("The model did not respond within %s. %s", e.Timeout, hint)
}

// requestTimeoutModel wraps a [fantasy.LanguageModel] so requests are
// bounded by the configured request_timeout. Non-streaming calls get a hard
// per-request deadline, applied per call so fantasy's retry loop gives every
// attempt a fresh budget — the same per-request semantics the provider SDKs
// expose. Streams instead get an idle timeout: the budget resets whenever a
// part arrives and only fires when the provider goes silent, so a slow but
// actively streaming response is never aborted.
type requestTimeoutModel struct {
	fantasy.LanguageModel
	timeout time.Duration
}

// newRequestTimeoutModel bounds each request to m with the given timeout. A
// timeout of zero or less, or a nil model, returns m unchanged.
func newRequestTimeoutModel(m fantasy.LanguageModel, timeout time.Duration) fantasy.LanguageModel {
	if m == nil || timeout <= 0 {
		return m
	}
	return requestTimeoutModel{LanguageModel: m, timeout: timeout}
}

// wrapTimedOut replaces err with the RequestTimeoutError when this model's
// own deadline fired. Other errors — user cancellation, outer deadlines,
// provider failures — pass through unchanged. Cancellation errors caused by
// our own timer report as [context.DeadlineExceeded] so callers never
// mistake a timeout for a user cancellation.
func wrapTimedOut(ctx context.Context, timeoutErr *RequestTimeoutError, err error) error {
	if err == nil || context.Cause(ctx) != timeoutErr {
		return err
	}
	if errors.Is(err, context.Canceled) {
		timeoutErr.Cause = context.DeadlineExceeded
	} else {
		timeoutErr.Cause = err
	}
	return timeoutErr
}

// Generate implements [fantasy.LanguageModel]. The request gets a hard
// deadline: there is no incremental progress signal, so the whole call must
// finish within the budget.
func (m requestTimeoutModel) Generate(ctx context.Context, call fantasy.Call) (*fantasy.Response, error) {
	timeoutErr := &RequestTimeoutError{Timeout: m.timeout}
	ctx, cancel := context.WithTimeoutCause(ctx, m.timeout, timeoutErr)
	defer cancel()
	resp, err := m.LanguageModel.Generate(ctx, call)
	return resp, wrapTimedOut(ctx, timeoutErr, err)
}

// Stream implements [fantasy.LanguageModel].
//
// The stream is consumed after Stream returns, so the timer must outlive
// this call: it fires only after timeout seconds without any part arriving,
// and is released when iteration ends, whether the stream finishes, breaks,
// or the idle timeout aborts it. Both the initial connection and gaps
// between parts share the same budget.
func (m requestTimeoutModel) Stream(ctx context.Context, call fantasy.Call) (fantasy.StreamResponse, error) {
	timeoutErr := &RequestTimeoutError{Timeout: m.timeout, Idle: true}
	ctx, cancel := context.WithCancelCause(ctx)
	timer := time.AfterFunc(m.timeout, func() { cancel(timeoutErr) })

	inner, err := m.LanguageModel.Stream(ctx, call)
	if err != nil {
		timer.Stop()
		cancel(nil)
		return nil, wrapTimedOut(ctx, timeoutErr, err)
	}
	return func(yield func(fantasy.StreamPart) bool) {
		defer timer.Stop()
		defer cancel(nil)
		inner(func(part fantasy.StreamPart) bool {
			timer.Reset(m.timeout)
			part.Error = wrapTimedOut(ctx, timeoutErr, part.Error)
			return yield(part)
		})
	}, nil
}

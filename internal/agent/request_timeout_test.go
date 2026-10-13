package agent

import (
	"context"
	"runtime"
	"testing"
	"time"

	"charm.land/fantasy"
	"charm.land/x/vcr"
	"github.com/charmbracelet/crush/internal/agent/providers"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/stretchr/testify/require"
)

// fakeLanguageModel is a [fantasy.LanguageModel] stub that records the
// context its methods were called with and can be configured with a custom
// stream body.
type fakeLanguageModel struct {
	generateCtx context.Context
	streamCtx   context.Context
	stream      func(yield func(fantasy.StreamPart) bool)
}

func (f *fakeLanguageModel) Generate(ctx context.Context, _ fantasy.Call) (*fantasy.Response, error) {
	f.generateCtx = ctx
	return &fantasy.Response{}, nil
}

func (f *fakeLanguageModel) Stream(ctx context.Context, _ fantasy.Call) (fantasy.StreamResponse, error) {
	f.streamCtx = ctx
	if f.stream == nil {
		return func(yield func(fantasy.StreamPart) bool) {
			yield(fantasy.StreamPart{})
		}, nil
	}
	return f.stream, nil
}

func (f *fakeLanguageModel) GenerateObject(context.Context, fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	return &fantasy.ObjectResponse{}, nil
}

func (f *fakeLanguageModel) StreamObject(context.Context, fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	return nil, nil
}

func (f *fakeLanguageModel) Provider() string { return "fake" }
func (f *fakeLanguageModel) Model() string    { return "fake-model" }

// timeoutOnlyModel streams a single error part shaped exactly like the one
// requestTimeoutModel produces when its deadline fires.
type timeoutOnlyModel struct {
	fakeLanguageModel
}

func (m *timeoutOnlyModel) Stream(context.Context, fantasy.Call) (fantasy.StreamResponse, error) {
	timeoutErr := &providers.RequestTimeoutError{Timeout: time.Second, Idle: true, Cause: context.DeadlineExceeded}
	return func(yield func(fantasy.StreamPart) bool) {
		yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeError, Error: timeoutErr})
	}, nil
}

// TestRequestTimeoutRunFinishMessage pins what the user sees when a request
// exhausts its timeout: a "Request timed out" finish that names the elapsed
// budget and how to change it, instead of a bare provider error.
func TestRequestTimeoutRunFinishMessage(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows for now")
	}

	env := testEnv(t)
	model := &timeoutOnlyModel{}
	agent, err := coderAgent(vcr.NewRecorder(t), env, model, model)
	require.NoError(t, err)

	session, err := env.sessions.Create(t.Context(), "timeout session")
	require.NoError(t, err)

	_, err = agent.Run(t.Context(), SessionAgentCall{
		Prompt:          "Hello",
		SessionID:       session.ID,
		MaxOutputTokens: 10000,
	})
	require.Error(t, err)

	msgs, err := env.messages.List(t.Context(), session.ID)
	require.NoError(t, err)

	var finish *message.Finish
	for _, msg := range msgs {
		if msg.Role != message.Assistant {
			continue
		}
		if part := msg.FinishPart(); part != nil {
			finish = part
		}
	}
	require.NotNil(t, finish, "the assistant message should carry a finish part")
	require.Equal(t, message.FinishReasonError, finish.Reason)
	require.Equal(t, "Request timed out", finish.Message)
	require.Contains(t, finish.Details, "stopped sending data for 1s")
	require.Contains(t, finish.Details, "request-timeout")
}

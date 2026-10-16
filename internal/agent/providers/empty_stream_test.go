package providers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"charm.land/fantasy"
	"charm.land/fantasy/providers/google"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/stretchr/testify/require"
)

type emptyStreamTestModel struct {
	fakeLanguageModel
	responses [][]fantasy.StreamPart
	calls     []fantasy.Call
}

func (m *emptyStreamTestModel) Stream(_ context.Context, call fantasy.Call) (fantasy.StreamResponse, error) {
	index := len(m.calls)
	m.calls = append(m.calls, call)
	return func(yield func(fantasy.StreamPart) bool) {
		for _, part := range m.responses[min(index, len(m.responses)-1)] {
			if !yield(part) {
				return
			}
		}
	}, nil
}

func TestEmptyStreamRecovery(t *testing.T) {
	for _, test := range []struct {
		name  string
		parts []fantasy.StreamPart
		retry bool
	}{
		{name: "no chunks", retry: true},
		{name: "unknown finish", parts: []fantasy.StreamPart{{Type: fantasy.StreamPartTypeFinish, FinishReason: fantasy.FinishReasonUnknown}}, retry: true},
		{name: "keepalive only", parts: []fantasy.StreamPart{{Type: fantasy.StreamPartTypeKeepalive}}, retry: true},
		{name: "normal stop", parts: []fantasy.StreamPart{{Type: fantasy.StreamPartTypeFinish, FinishReason: fantasy.FinishReasonStop}}},
		{name: "refusal", parts: []fantasy.StreamPart{{Type: fantasy.StreamPartTypeFinish, FinishReason: fantasy.FinishReasonContentFilter}}},
		{name: "length limit", parts: []fantasy.StreamPart{{Type: fantasy.StreamPartTypeFinish, FinishReason: fantasy.FinishReasonLength}}},
		{name: "tool input", parts: []fantasy.StreamPart{{Type: fantasy.StreamPartTypeToolInputStart, ID: "tool"}}},
		{name: "partial text", parts: []fantasy.StreamPart{{Type: fantasy.StreamPartTypeTextDelta, Delta: "partial"}}},
		{name: "provider error", parts: []fantasy.StreamPart{{Type: fantasy.StreamPartTypeError, Error: errors.New("failed")}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			inner := &emptyStreamTestModel{responses: [][]fantasy.StreamPart{test.parts}}
			call := fantasy.Call{Prompt: []fantasy.Message{fantasy.NewUserMessage("continue the task")}}
			stream, err := (emptyStreamModel{inner}).Stream(t.Context(), call)
			require.NoError(t, err)
			var got []fantasy.StreamPart
			for part := range stream {
				got = append(got, part)
			}
			if test.retry {
				last := got[len(got)-1]
				require.Equal(t, fantasy.StreamPartTypeError, last.Type)
				var providerErr *fantasy.ProviderError
				require.ErrorAs(t, last.Error, &providerErr)
				require.True(t, providerErr.IsRetryable())
			} else {
				require.Len(t, inner.calls, 1)
				require.Equal(t, test.parts, got)
			}
		})
	}
}

func TestEmptyStreamRepeatedFailureIsReported(t *testing.T) {
	inner := &emptyStreamTestModel{responses: [][]fantasy.StreamPart{nil}}
	result, err := fantasy.NewAgent(emptyStreamModel{inner}, fantasy.WithMaxRetries(1)).Stream(t.Context(), fantasy.AgentStreamCall{Prompt: "finish the task"})
	require.ErrorContains(t, err, "unexpected EOF")
	require.Nil(t, result)
	require.Len(t, inner.calls, 2, "recovery must be bounded")
}

func TestEmptyStreamCancellationDoesNotRetry(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	inner := &emptyStreamTestModel{responses: [][]fantasy.StreamPart{nil}}
	stream, err := (emptyStreamModel{inner}).Stream(ctx, fantasy.Call{})
	require.NoError(t, err)
	cancel()
	for part := range stream {
		require.ErrorIs(t, part.Error, context.Canceled)
	}
	require.Len(t, inner.calls, 1)
}

func TestEmptyStreamAfterThirteenActionsKeepsWorking(t *testing.T) {
	inner := &emptyStreamTestModel{}
	for number := 0; number < 13; number++ {
		inner.responses = append(inner.responses, []fantasy.StreamPart{
			{Type: fantasy.StreamPartTypeToolCall, ID: fmt.Sprintf("work-%d", number), ToolCallName: "work", ToolCallInput: "{}"},
			{Type: fantasy.StreamPartTypeFinish, FinishReason: fantasy.FinishReasonToolCalls},
		})
	}
	inner.responses = append(inner.responses,
		[]fantasy.StreamPart{{Type: fantasy.StreamPartTypeFinish, FinishReason: fantasy.FinishReasonUnknown}},
		[]fantasy.StreamPart{
			{Type: fantasy.StreamPartTypeTextStart, ID: "answer"},
			{Type: fantasy.StreamPartTypeTextDelta, ID: "answer", Delta: "finished the task"},
			{Type: fantasy.StreamPartTypeTextEnd, ID: "answer"},
			{Type: fantasy.StreamPartTypeFinish, FinishReason: fantasy.FinishReasonStop},
		},
	)
	workCount := 0
	tool := fantasy.NewAgentTool("work", "do work", func(context.Context, struct{}, fantasy.ToolCall) (fantasy.ToolResponse, error) {
		workCount++
		return fantasy.NewTextResponse("done"), nil
	})
	result, err := fantasy.NewAgent(emptyStreamModel{inner}, fantasy.WithTools(tool)).Stream(t.Context(), fantasy.AgentStreamCall{Prompt: "do the whole task"})
	require.NoError(t, err)
	require.Equal(t, "finished the task", result.Response.Content.Text())
	require.Equal(t, 13, workCount, "retry must not repeat completed actions")
	require.Len(t, inner.calls, 15)
	require.Equal(t, inner.calls[13], inner.calls[14], "resume with every earlier tool result intact")
}

func TestGeminiRateLimitWaitsAndContinues(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			fmt.Fprint(w, `{"error":{"code":429,"status":"RESOURCE_EXHAUSTED","message":"Too many requests. Please retry."}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"finished after waiting\"}]},\"finishReason\":\"STOP\"}]}\n\n")
	}))
	defer server.Close()
	provider, err := google.New(google.WithBaseURL(server.URL), google.WithGeminiAPIKey("test-key"))
	require.NoError(t, err)
	builder := Builder{Config: config.NewTestStore(&config.Config{Options: &config.Options{}})}
	model, err := builder.LanguageModel(t.Context(), provider, config.ProviderConfig{Type: "google"}, config.SelectedModel{Provider: "gemini", Model: "gemini-4-argon"})
	require.NoError(t, err)
	retries := 0
	result, err := fantasy.NewAgent(model).Stream(t.Context(), fantasy.AgentStreamCall{
		Prompt: "finish the task",
		OnRetry: func(err *fantasy.ProviderError, delay time.Duration) {
			require.Equal(t, http.StatusTooManyRequests, err.StatusCode)
			require.Positive(t, delay, "rate limits must wait before trying again")
			retries++
		},
	})
	require.NoError(t, err)
	require.Equal(t, "finished after waiting", result.Response.Content.Text())
	require.EqualValues(t, 2, requests.Load())
	require.Equal(t, 1, retries)
}

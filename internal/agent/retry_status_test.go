package agent

import (
	"net/http"
	"testing"
	"time"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/stretchr/testify/require"
)

func TestRetryStatusLastsOnlyDuringWait(t *testing.T) {
	for _, test := range []struct {
		name     string
		err      *fantasy.ProviderError
		activity string
	}{
		{"rate limit", &fantasy.ProviderError{StatusCode: http.StatusTooManyRequests}, "rate_limited"},
		{"server error", &fantasy.ProviderError{StatusCode: http.StatusServiceUnavailable}, "retrying"},
		{"interrupted response", nil, "retrying"},
	} {
		t.Run(test.name, func(t *testing.T) {
			env := testEnv(t)
			a := testSessionAgent(env, fastModel{}, fastModel{}, "system").(*sessionAgent)
			sess, err := env.sessions.Create(t.Context(), "retry status")
			require.NoError(t, err)
			msg, err := env.messages.Create(t.Context(), sess.ID, message.CreateMessageParams{Role: message.Assistant})
			require.NoError(t, err)
			msg.Activity, msg.ActivityDetail = "thinking", "Old thought"
			msg.AppendReasoningContent("Partial thought")
			turn := &turn{a: a, genCtx: t.Context(), currentAssistant: &msg, chatModel: a.model.Get()}
			turn.onRetry(test.err, time.Second)
			require.Equal(t, test.activity, msg.Activity)
			require.Empty(t, msg.ActivityDetail)
			require.False(t, msg.IsThinking())
			require.NotNil(t, turn.modelProvider())
			require.Empty(t, msg.Activity, "the next request must clear the waiting notice before it streams")
		})
	}
}

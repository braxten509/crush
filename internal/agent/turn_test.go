package agent

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/agent/providers"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/stretchr/testify/require"
)

func TestClassifyRunError(t *testing.T) {
	t.Parallel()

	timeout := &providers.RequestTimeoutError{Timeout: time.Second, Cause: context.DeadlineExceeded}
	for _, test := range []struct {
		name           string
		err            error
		isHyper        bool
		reason         message.FinishReason
		title, details string
	}{
		{"canceled", fmt.Errorf("stream: %w", context.Canceled), false, message.FinishReasonCanceled, "User canceled request", ""},
		{"request timeout", fmt.Errorf("stream: %w", timeout), false, message.FinishReasonError, "Request timed out", timeout.UserMessage()},
		{"hyper unauthorized", &fantasy.ProviderError{StatusCode: http.StatusUnauthorized, Title: "unauthorized", Message: "bad key"}, true, message.FinishReasonError, "Unauthorized", `Please re-authenticate with Hyper. You can also run "crush auth" to re-authenticate.`},
		{"other unauthorized", &fantasy.ProviderError{StatusCode: http.StatusUnauthorized, Title: "unauthorized", Message: "bad key"}, false, message.FinishReasonError, "Unauthorized", "bad key"},
		{"provider error without title", &fantasy.ProviderError{Message: "overloaded"}, false, message.FinishReasonError, "Provider Error", "overloaded"},
		{"fantasy error", &fantasy.Error{Title: "bad request", Message: "nope"}, false, message.FinishReasonError, "Bad Request", "nope"},
		{"other", errors.New("boom"), false, message.FinishReasonError, "Provider Error", "boom"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			reason, title, details := classifyRunError(test.err, test.isHyper, "Model")
			require.Equal(t, test.reason, reason)
			require.Equal(t, test.title, title)
			require.Equal(t, test.details, details)
		})
	}

	_, title, details := classifyRunError(&fantasy.ProviderError{Message: "The requested model is not supported."}, false, "GPT Model")
	require.Equal(t, "Copilot model not enabled", title)
	require.Contains(t, details, `"GPT Model" is not enabled in Copilot.`)
	require.Contains(t, details, "https://github.com/settings/copilot/features")
}

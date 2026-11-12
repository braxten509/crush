package agent

import (
	"charm.land/catwalk/v3/pkg/catwalk"
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/agent/providers"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/stretchr/testify/require"
)

func TestClassifyRunError(t *testing.T) {
	t.Parallel()

	timeout := &providers.RequestTimeoutError{Timeout: time.Second, Cause: context.DeadlineExceeded}
	for _, test := range []struct {
		name       string
		err        error
		isHyper    bool
		reason     message.FinishReason
		detailsHas string // what the details must keep, such as the provider's message
	}{
		{"canceled", fmt.Errorf("stream: %w", context.Canceled), false, message.FinishReasonCanceled, ""},
		{"request timeout", fmt.Errorf("stream: %w", timeout), false, message.FinishReasonError, timeout.UserMessage()},
		{"hyper unauthorized", &fantasy.ProviderError{StatusCode: http.StatusUnauthorized, Title: "unauthorized", Message: "bad key"}, true, message.FinishReasonError, "crush login hyper"},
		{"other unauthorized", &fantasy.ProviderError{StatusCode: http.StatusUnauthorized, Title: "unauthorized", Message: "bad key"}, false, message.FinishReasonError, "bad key"},
		{"provider error without title", &fantasy.ProviderError{Message: "overloaded"}, false, message.FinishReasonError, "overloaded"},
		{"fantasy error", &fantasy.Error{Title: "bad request", Message: "nope"}, false, message.FinishReasonError, "nope"},
		{"other", errors.New("boom"), false, message.FinishReasonError, "boom"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			reason, title, details := classifyRunError(test.err, Model{OAuth: test.isHyper, ProviderName: "Hyper", ModelCfg: config.SelectedModel{Provider: "hyper"}, CatwalkCfg: catwalk.Model{Name: "Model"}})
			require.Equal(t, test.reason, reason)
			require.NotEmpty(t, title)
			require.Contains(t, details, test.detailsHas)
		})
	}

	_, _, details := classifyRunError(&fantasy.ProviderError{Message: "The requested model is not supported."}, Model{CatwalkCfg: catwalk.Model{Name: "GPT Model"}})
	require.Contains(t, details, "https://github.com/settings/copilot/features")
}

package auth

import (
	"context"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/oauth/copilot"
	"github.com/charmbracelet/crush/internal/ui/common"
)

func NewOAuthCopilot(
	com *common.Common,
	isOnboarding bool,
	provider catwalk.Provider,
	model config.SelectedModel,
	modelType config.SelectedModelType,
) (*OAuth, tea.Cmd) {
	return newOAuth(com, isOnboarding, provider, model, modelType, &OAuthCopilot{})
}

type OAuthCopilot struct {
	deviceCode *copilot.DeviceCode
	cancelFunc func()
}

var _ OAuthProvider = (*OAuthCopilot)(nil)

func (m *OAuthCopilot) name() string {
	return "GitHub Copilot"
}

func (m *OAuthCopilot) initiateAuth() tea.Msg {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	deviceCode, err := copilot.RequestDeviceCode(ctx)
	if err != nil {
		return ActionOAuthErrored{Error: fmt.Errorf("failed to initiate device auth: %w", err)}
	}

	return ActionInitiateOAuth{
		DeviceCode:      deviceCode.DeviceCode,
		UserCode:        deviceCode.UserCode,
		VerificationURL: deviceCode.VerificationURI,
		ExpiresIn:       deviceCode.ExpiresIn,
		Interval:        deviceCode.Interval,
		flowState:       deviceCode,
	}
}

func (m *OAuthCopilot) startPolling(msg ActionInitiateOAuth) tea.Cmd {
	if deviceCode, ok := msg.flowState.(*copilot.DeviceCode); ok {
		m.deviceCode = deviceCode
	}
	if m.cancelFunc != nil {
		m.cancelFunc()
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.cancelFunc = cancel
	deviceCode := m.deviceCode

	return func() tea.Msg {
		token, err := copilot.PollForToken(ctx, deviceCode)
		if err != nil {
			if ctx.Err() != nil {
				return nil // cancelled, don't report error.
			}
			return ActionOAuthErrored{Error: err}
		}

		return ActionCompleteOAuth{Token: token}
	}
}

func (m *OAuthCopilot) stopPolling() tea.Cmd {
	cancel := m.cancelFunc
	m.cancelFunc = nil
	return func() tea.Msg {
		if cancel != nil {
			cancel()
		}
		return nil
	}
}

// supportsCodeEntry reports whether the provider's flow can finish with
// a pasted code. Only the Grok browser flow offers the manual fallback.
func (m *OAuthCopilot) supportsCodeEntry() bool {
	return false
}

// submitCode is unreachable for providers without code entry.
func (m *OAuthCopilot) submitCode(string) tea.Cmd {
	return nil
}

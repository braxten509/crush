package auth

import (
	"context"
	"fmt"

	tea "charm.land/bubbletea/v2"
	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/oauth/grok"
	"github.com/charmbracelet/crush/internal/ui/common"
)

// NewOAuthGrok creates an OAuth dialog for signing in with a Grok
// account. The sign-in starts with the ChatGPT-style browser flow; when
// the user declines that permission (or the loopback callback is out of
// reach), it falls back to the device flow, showing a code the user
// enters on accounts.x.ai.
func NewOAuthGrok(
	com *common.Common,
	isOnboarding bool,
	provider catwalk.Provider,
	model config.SelectedModel,
	modelType config.SelectedModelType,
) (*OAuth, tea.Cmd) {
	return newOAuth(com, isOnboarding, provider, model, modelType, &OAuthGrok{})
}

type OAuthGrok struct {
	flow *grok.BrowserFlow
	// cancelFunc stops the running poll (device flow or browser wait).
	cancelFunc context.CancelFunc
	// codeCancel stops an in-flight exchange of a pasted code. It is
	// separate from cancelFunc so submitting a code does not orphan the
	// browser wait that is still running alongside it.
	codeCancel context.CancelFunc
}

var _ OAuthProvider = (*OAuthGrok)(nil)

func (m *OAuthGrok) name() string {
	return "Grok"
}

func (m *OAuthGrok) initiateAuth() tea.Msg {
	flow, err := grok.StartBrowserFlow()
	if err == nil {
		return ActionInitiateOAuth{
			VerificationURL: flow.StartURL(),
			flowState:       flow,
		}
	}

	// The loopback callback listener could not start (port unavailable,
	// remote machine): go straight to the device flow, where the user
	// opens the verification page and enters the code themselves.
	deviceCode, deviceErr := grok.RequestDeviceCode(context.Background())
	if deviceErr != nil {
		return ActionOAuthErrored{Error: fmt.Errorf("failed to start browser auth: %w (device auth: %v)", err, deviceErr)}
	}
	return deviceCodeInitiate(deviceCode)
}

func (m *OAuthGrok) startPolling(msg ActionInitiateOAuth) tea.Cmd {
	if flow, ok := msg.flowState.(*grok.BrowserFlow); ok {
		m.flow = flow
	}
	if m.cancelFunc != nil {
		m.cancelFunc()
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.cancelFunc = cancel
	flow := m.flow
	deviceCode, expiresIn := msg.DeviceCode, msg.ExpiresIn

	return func() tea.Msg {
		if deviceCode != "" {
			token, err := grok.PollForToken(ctx, deviceCode, expiresIn)
			if err != nil {
				if ctx.Err() != nil {
					return nil // cancelled, don't report error.
				}
				return ActionOAuthErrored{Error: err}
			}
			return ActionCompleteOAuth{Token: token}
		}

		token, err := flow.Wait(ctx)
		if err == nil {
			return ActionCompleteOAuth{Token: token}
		}
		if ctx.Err() != nil {
			return nil // cancelled, don't report error.
		}

		// The browser permission was declined (or the callback never
		// arrived): offer the device flow instead, whose code the user
		// enters on accounts.x.ai to finish signing in.
		deviceCode, deviceErr := grok.RequestDeviceCode(ctx)
		if deviceErr != nil {
			return ActionOAuthErrored{Error: err}
		}
		return deviceCodeInitiate(deviceCode)
	}
}

func (m *OAuthGrok) stopPolling() tea.Cmd {
	cancel, codeCancel, flow := m.cancelFunc, m.codeCancel, m.flow
	m.cancelFunc, m.codeCancel = nil, nil
	return func() tea.Msg {
		if cancel != nil {
			cancel()
		}
		if codeCancel != nil {
			codeCancel()
		}
		if flow != nil {
			flow.Close()
		}
		return nil
	}
}

func (m *OAuthGrok) supportsCodeEntry() bool {
	return true
}

func (m *OAuthGrok) submitCode(input string) tea.Cmd {
	flow := m.flow
	if flow == nil {
		return func() tea.Msg {
			return ActionOAuthErrored{Error: fmt.Errorf("no browser flow in progress")}
		}
	}
	if m.codeCancel != nil {
		m.codeCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.codeCancel = cancel

	return func() tea.Msg {
		token, err := flow.CompleteWithCode(ctx, input)
		if err != nil {
			if ctx.Err() != nil {
				return nil // cancelled, don't report error.
			}
			return ActionOAuthErrored{Error: err}
		}
		return ActionCompleteOAuth{Token: token}
	}
}

// deviceCodeInitiate turns a device authorization into the message that
// moves the dialog to its code-display state.
func deviceCodeInitiate(deviceCode *grok.DeviceCode) tea.Msg {
	return ActionInitiateOAuth{
		DeviceCode:      deviceCode.DeviceCode,
		UserCode:        deviceCode.UserCode,
		VerificationURL: deviceCode.VerificationURI,
		ExpiresIn:       deviceCode.ExpiresIn,
		Interval:        deviceCode.Interval,
	}
}

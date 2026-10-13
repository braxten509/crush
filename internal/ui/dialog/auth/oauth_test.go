package auth

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/oauth/copilot"
	"github.com/charmbracelet/crush/internal/oauth/grok"
	"github.com/stretchr/testify/require"
)

// runCmdAsync runs cmd off the calling goroutine, the way Bubble Tea does,
// and returns a channel that receives its message.
func runCmdAsync(cmd tea.Cmd) <-chan tea.Msg {
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	return done
}

func waitMsg(t *testing.T, done <-chan tea.Msg) tea.Msg {
	t.Helper()
	select {
	case msg := <-done:
		return msg
	case <-time.After(5 * time.Second):
		t.Fatal("command did not return after the flow was stopped")
		return nil
	}
}

// TestOAuthGrokInitiateAuthLeavesProviderUntouched checks that the
// initiating command, which runs off the update loop, hands the browser
// flow back in its message instead of storing it on the provider.
func TestOAuthGrokInitiateAuthLeavesProviderUntouched(t *testing.T) {
	t.Parallel()

	m := &OAuthGrok{}
	msg, ok := m.initiateAuth().(ActionInitiateOAuth)
	require.True(t, ok)
	flow, ok := msg.flowState.(*grok.BrowserFlow)
	require.True(t, ok)
	t.Cleanup(flow.Close)
	require.Nil(t, m.flow)
	require.Nil(t, m.cancelFunc)
}

// TestOAuthGrokStopPollingCancelsWait checks that the poll's cancel func
// is in place as soon as startPolling returns, so stopping the flow
// unblocks the running poll.
func TestOAuthGrokStopPollingCancelsWait(t *testing.T) {
	t.Parallel()

	flow, err := grok.StartBrowserFlow()
	require.NoError(t, err)
	t.Cleanup(flow.Close)

	m := &OAuthGrok{}
	cmd := m.startPolling(ActionInitiateOAuth{VerificationURL: flow.StartURL(), flowState: flow})
	require.Same(t, flow, m.flow)
	require.NotNil(t, m.cancelFunc)

	done := runCmdAsync(cmd)
	stop := m.stopPolling()
	require.Nil(t, m.cancelFunc)
	require.Nil(t, stop())
	require.Nil(t, waitMsg(t, done))
}

// TestOAuthGrokSubmitCodeKeepsWaitCancellable checks that submitting a
// pasted code does not replace the browser wait's cancel func, which used
// to leave that wait blocked forever once the dialog closed.
func TestOAuthGrokSubmitCodeKeepsWaitCancellable(t *testing.T) {
	t.Parallel()

	flow, err := grok.StartBrowserFlow()
	require.NoError(t, err)
	t.Cleanup(flow.Close)

	m := &OAuthGrok{}
	done := runCmdAsync(m.startPolling(ActionInitiateOAuth{flowState: flow}))

	// The returned command would exchange the code over the network, so
	// it is not run; building it must already register its cancel func.
	require.NotNil(t, m.submitCode("code"))
	require.NotNil(t, m.codeCancel)
	require.NotNil(t, m.cancelFunc)

	m.stopPolling()()
	require.Nil(t, waitMsg(t, done))
}

func TestOAuthGrokSubmitCodeWithoutFlow(t *testing.T) {
	t.Parallel()

	m := &OAuthGrok{}
	msg, ok := m.submitCode("code")().(ActionOAuthErrored)
	require.True(t, ok)
	require.EqualError(t, msg.Error, "no browser flow in progress")
	require.Nil(t, m.codeCancel)
}

// TestOAuthCopilotStartPollingStoresDeviceCode checks that the device
// code travels in the message and is stored on the update loop.
func TestOAuthCopilotStartPollingStoresDeviceCode(t *testing.T) {
	t.Parallel()

	deviceCode := &copilot.DeviceCode{DeviceCode: "dc", UserCode: "uc"}
	m := &OAuthCopilot{}
	require.NotNil(t, m.startPolling(ActionInitiateOAuth{DeviceCode: "dc", flowState: deviceCode}))
	require.Same(t, deviceCode, m.deviceCode)
	require.NotNil(t, m.cancelFunc)

	m.stopPolling()()
	require.Nil(t, m.cancelFunc)
}

func TestOAuthHyperStartPollingSetsCancelBeforeCmdRuns(t *testing.T) {
	t.Parallel()

	m := &OAuthHyper{}
	require.NotNil(t, m.startPolling(ActionInitiateOAuth{DeviceCode: "dc", ExpiresIn: 1}))
	require.NotNil(t, m.cancelFunc)

	m.stopPolling()()
	require.Nil(t, m.cancelFunc)
}

func TestOAuthOpenAIStopPollingWithoutFlow(t *testing.T) {
	t.Parallel()

	m := &OAuthOpenAI{}
	require.NotNil(t, m.startPolling(ActionInitiateOAuth{}))
	require.NotNil(t, m.cancelFunc)
	require.Nil(t, m.stopPolling()())
	require.Nil(t, m.cancelFunc)
}

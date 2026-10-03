package agent

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestQueuedPromptWakesCLISteering(t *testing.T) {
	t.Parallel()
	sa := NewSessionAgent(SessionAgentOptions{}).(*sessionAgent)
	s := &cliSteps{running: toolRunningForSteer(), ctx: t.Context(), a: sa, sessionID: "wake", steerReady: make(chan struct{}, 1)}
	sa.steering.Set(s.sessionID, s)
	// enqueueCall runs under the dispatch lock in Run. Its notification
	// must not wait for the driver's drain, which needs that same lock.
	lock := sa.sessionMu(s.sessionID)
	lock.Lock()
	sa.enqueueCall(SessionAgentCall{SessionID: s.sessionID, Prompt: "first"})
	sa.enqueueCall(SessionAgentCall{SessionID: s.sessionID, Prompt: "second"})
	lock.Unlock()
	select {
	case <-s.steerReady:
	default:
		t.Fatal("queue did not wake the running CLI")
	}
	require.Equal(t, "first\n\nsecond", s.steer())
	select {
	case <-s.steerReady:
		t.Fatal("notifications should coalesce while a drain is pending")
	default:
	}
}

// toolRunningForSteer marks a tool as running, the only time a turn takes
// queued prompts in.
func toolRunningForSteer() map[string]bool { return map[string]bool{"tool": true} }

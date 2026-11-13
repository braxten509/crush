package agent

import (
	"context"
	"testing"
	"time"

	"charm.land/catwalk/v3/pkg/catwalk"
	"charm.land/fantasy"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/charmbracelet/crush/internal/config"
)

// TestStopChatWorkEndsHelpersOfHelpers: closing a chat ends its helper and
// the helper's own helper, and no parent gets a turn about it. A stop that
// only looked one level down would leave the inner helper running.
func TestStopChatWorkEndsHelpersOfHelpers(t *testing.T) {
	notified := make(chan string, 4)
	c := sessionRunTestCoordinator(t, func(_ context.Context, call SessionAgentCall) (*fantasy.AgentResult, error) {
		notified <- call.Prompt
		return fakeSessionReply(call, "received"), nil
	})
	h := resumeTestHub(t, c)
	chat, helper := helperChats(t, c)
	inner, err := c.sessions.CreateTaskSession(t.Context(), uuid.NewString(), helper.ID, "inner")
	require.NoError(t, err)

	m := Model{CatwalkCfg: catwalk.Model{DefaultMaxTokens: 4096}}
	waits := newMockAgent("test", 4096, func(ctx context.Context, _ SessionAgentCall) (*fantasy.AgentResult, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
	start := func(parent, child string) chan struct{} {
		released := make(chan struct{})
		_, err := h.start(t.Context(), func() { close(released) }, &Task{SessionID: parent, ChildID: child, Name: "worker", Status: TaskRunning, Started: time.Now()}, waits, m, config.ProviderConfig{ID: "test"}, "work")
		require.NoError(t, err)
		return released
	}
	outerDone := start(chat.ID, helper.ID)
	innerDone := start(helper.ID, inner.ID)

	StopChatWork(c, chat.ID)

	for name, done := range map[string]chan struct{}{"the helper": outerDone, "the helper's helper": innerDone} {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatalf("%s still runs after its chat closed", name)
		}
	}
	select {
	case prompt := <-notified:
		t.Fatalf("a closed chat's helpers tell no one: %q", prompt)
	case <-time.After(300 * time.Millisecond):
	}
}

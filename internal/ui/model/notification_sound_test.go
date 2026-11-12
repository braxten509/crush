package model

import (
	"testing"

	"github.com/charmbracelet/crush/internal/agent/notify"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/ui/notification"
	"github.com/stretchr/testify/require"
)

func TestNotificationSoundPolicy(t *testing.T) {
	t.Parallel()
	for _, focused := range []bool{false, true} {
		for _, style := range []string{"", "auto", "native", "osc", "bell", "disabled"} {
			ui := newTestUIWithConfig(t, &config.Config{Options: &config.Options{Notifications: style}})
			ui.notifyWindowFocused = focused
			// Sound must also work in terminals without focus reporting.
			ui.caps.ReportFocusEvents = false
			for _, sound := range []notification.Sound{notification.SoundQuestion, notification.SoundComplete} {
				cmd := ui.playNotificationSound(sound)
				if style == "disabled" {
					require.Nil(t, cmd)
				} else {
					require.NotNil(t, cmd)
				}
			}
		}
	}
}

func TestCompletionSoundChecksFreshSessionState(t *testing.T) {
	for _, test := range []struct {
		name   string
		busy   bool
		queued []string
		alert  bool
	}{
		{name: "idle", alert: true},
		{name: "running", busy: true},
		{name: "queued", queued: []string{"follow-up"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			ws := &countingWorkspace{ready: true}
			ui := newBusyUI(ws)
			warmCaches(ui, false)
			n := notify.Notification{Type: notify.TypeAgentFinished, SessionID: "s1"}
			cmd := ui.checkAgentFinished(n)
			require.Zero(t, ws.syncProbes(), "probes must run off the UI thread")
			// Simulate a new run/queue after the event was handled but
			// before its command executes. The idle cache remains stale.
			ws.agentBusy, ws.queued = test.busy, test.queued
			msg := cmd()
			if test.alert {
				require.Equal(t, agentFinishedMsg{notification: n}, msg)
			} else {
				require.Nil(t, msg, "a stale completion must not schedule sound or a desktop notification")
			}
		})
	}
}

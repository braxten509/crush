package model

import (
	"testing"

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

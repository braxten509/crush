package chat

import (
	"fmt"
	"time"
)

// activityTimer measures the current activity from when the UI observes it.
// Provider heartbeats do not restart it; a new activity or tool call does.
type activityTimer struct {
	key     string
	started time.Time
}

func (t *activityTimer) update(key string, now time.Time) {
	if key != t.key {
		t.key = key
		t.started = now
	}
}

func (t *activityTimer) elapsed(now time.Time) string {
	if t.key == "" {
		return ""
	}
	seconds := max(0, int(now.Sub(t.started)/time.Second))
	switch {
	case seconds < 10:
		return ""
	case seconds >= 3600:
		return fmt.Sprintf("%dh %dm", seconds/3600, seconds/60%60)
	case seconds >= 60:
		return fmt.Sprintf("%dm %ds", seconds/60, seconds%60)
	default:
		return fmt.Sprintf("%ds", seconds)
	}
}

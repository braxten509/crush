package config

import (
	"fmt"
	"time"
)

// The time a command an agent runs may hold the turn before Crush moves it
// to the background (options.auto_background_seconds; owner, 2026-11-12:
// "let me configure it, in seconds, no decimals").
const (
	// DefaultAutoBackgroundSeconds is used until the user saves a time
	// (owner, 2026-10-21: "a command running 2 minutes or more is forced
	// to background").
	DefaultAutoBackgroundSeconds = 120
	// MinAutoBackgroundSeconds is the shortest time accepted: waits of up
	// to 10 seconds are always left in the foreground
	// (shelltool.ForegroundWaitLimit).
	MinAutoBackgroundSeconds = 11
	// MaxAutoBackgroundSeconds is the longest time accepted, one hour.
	MaxAutoBackgroundSeconds = 3600
)

// GetAutoBackground returns how long a command may hold the turn before it
// is moved to the background. The nil receiver and the unset field both
// mean DefaultAutoBackgroundSeconds; a saved value outside the accepted
// range is brought to its nearest end.
func (o *Options) GetAutoBackground() time.Duration {
	seconds := DefaultAutoBackgroundSeconds
	if o != nil && o.AutoBackgroundSeconds != nil {
		seconds = min(MaxAutoBackgroundSeconds, max(MinAutoBackgroundSeconds, *o.AutoBackgroundSeconds))
	}
	return time.Duration(seconds) * time.Second
}

// ValidateAutoBackgroundSeconds checks a user-entered time.
func ValidateAutoBackgroundSeconds(seconds int) error {
	if seconds < MinAutoBackgroundSeconds {
		return fmt.Errorf("Enter at least %d seconds", MinAutoBackgroundSeconds)
	}
	if seconds > MaxAutoBackgroundSeconds {
		return fmt.Errorf("Enter at most %d seconds", MaxAutoBackgroundSeconds)
	}
	return nil
}

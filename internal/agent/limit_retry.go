package agent

import (
	"context"
	"errors"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// limitRetryPrompt is what a chat is sent once its usage limit resets.
const limitRetryPrompt = "continue"

// limitRetryDelay is how long after a usage limit resets the chat is
// continued, so the provider has surely reset by then.
const limitRetryDelay = time.Minute

// maxLimitWait is the longest wait read as a reset time: weekly limits.
const maxLimitWait = 8 * 24 * time.Hour

// limitRetryPoll is how often a planned retry checks the wall clock. Go
// timers stop while the computer sleeps; the wall clock does not.
const limitRetryPoll = 30 * time.Second

// The words a provider puts before a reset time, like Claude Code's
// "resets 5:20am (America/Boise)" or Codex's "try again at 5:20 PM".
const resetWords = `\b(?:resets?|try again|available again|retry)\s+`

var (
	// resetClockPattern reads a clock time, with an optional weekday or
	// date before it and an optional time zone after it in parentheses.
	resetClockPattern = regexp.MustCompile(`(?i)` + resetWords + `(?:at\s+|on\s+)?` +
		`(?:(mon|tue|wed|thu|fri|sat|sun)[a-z]*\.?,?\s+)?` +
		`(?:(jan|feb|mar|apr|may|jun|jul|aug|sep|oct|nov|dec)[a-z]*\.?\s+(\d{1,2})(?:st|nd|rd|th)?,?\s+(?:(\d{4}),?\s+)?(?:at\s+)?)?` +
		`(\d{1,2})(?::(\d{2}))?\s*(?:([ap])\.?m\b\.?)?` +
		`(?:\s*\(([A-Za-z_]+(?:/[A-Za-z0-9_+\-]+)*)\))?`)
	// resetStampPattern reads a full date and time like 2026-10-20T05:20:00Z.
	resetStampPattern = regexp.MustCompile(`(?i)` + resetWords + `(?:at\s+|on\s+)?` +
		`(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}(?::\d{2}(?:\.\d+)?)?(?:Z|[+-]\d{2}:?\d{2}))`)
	// resetUnixPattern reads Claude Code's older "limit reached|1760000000".
	resetUnixPattern = regexp.MustCompile(`(?i)limit reached\|(\d{10})\b`)
	// resetAfterPattern reads a wait like "try again in 2 hours 5 minutes".
	resetAfterPattern = regexp.MustCompile(`(?i)` + resetWords + `in\s+((?:\d+\s*(?:days?|d|hours?|hrs?|h|minutes?|mins?|m|seconds?|secs?|s)\b[\s,]*(?:and\s+)?)+)`)
	resetAfterPart    = regexp.MustCompile(`(?i)(\d+)\s*([a-z]+)`)
)

var resetMonths = map[string]time.Month{
	"jan": time.January, "feb": time.February, "mar": time.March, "apr": time.April,
	"may": time.May, "jun": time.June, "jul": time.July, "aug": time.August,
	"sep": time.September, "oct": time.October, "nov": time.November, "dec": time.December,
}

var resetWeekdays = map[string]time.Weekday{
	"sun": time.Sunday, "mon": time.Monday, "tue": time.Tuesday, "wed": time.Wednesday,
	"thu": time.Thursday, "fri": time.Friday, "sat": time.Saturday,
}

// limitRetryTime is when a chat whose turn failed with err should be
// continued: one minute after the reset time the error names. It reports
// false when the error names none.
func limitRetryTime(err error, now time.Time) (time.Time, bool) {
	if err == nil || errors.Is(err, context.Canceled) {
		return time.Time{}, false
	}
	reset, ok := resetTime(err.Error(), now)
	if !ok {
		return time.Time{}, false
	}
	// Whole minutes, so the time shown and the time used always agree.
	at := reset.Add(limitRetryDelay).Truncate(time.Minute)
	// A reset time already past still waits a minute, so a provider that
	// keeps naming one can't start a fast loop.
	if earliest := now.Add(limitRetryDelay); at.Before(earliest) {
		at = earliest
	}
	if at.Sub(now) > maxLimitWait {
		return time.Time{}, false
	}
	return at, true
}

// resetTime reads the time a usage limit resets from an error message.
func resetTime(text string, now time.Time) (time.Time, bool) {
	if m := resetStampPattern.FindStringSubmatch(text); m != nil {
		for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04Z07:00", "2006-01-02T15:04:05Z0700", "2006-01-02T15:04Z0700"} {
			if t, err := time.Parse(layout, m[1]); err == nil {
				return t, true
			}
		}
	}
	if m := resetUnixPattern.FindStringSubmatch(text); m != nil {
		if secs, err := strconv.ParseInt(m[1], 10, 64); err == nil {
			return time.Unix(secs, 0), true
		}
	}
	for _, m := range resetClockPattern.FindAllStringSubmatch(text, -1) {
		if t, ok := clockResetTime(m, now); ok {
			return t, true
		}
	}
	if m := resetAfterPattern.FindStringSubmatch(text); m != nil {
		var wait time.Duration
		for _, part := range resetAfterPart.FindAllStringSubmatch(m[1], -1) {
			n, _ := strconv.Atoi(part[1])
			unit := strings.ToLower(part[2])
			switch {
			case strings.HasPrefix(unit, "d"):
				wait += time.Duration(n) * 24 * time.Hour
			case strings.HasPrefix(unit, "h"):
				wait += time.Duration(n) * time.Hour
			case strings.HasPrefix(unit, "m"):
				wait += time.Duration(n) * time.Minute
			case strings.HasPrefix(unit, "s"):
				wait += time.Duration(n) * time.Second
			}
		}
		if wait > 0 {
			return now.Add(wait), true
		}
	}
	return time.Time{}, false
}

// clockResetTime turns one resetClockPattern match into a time. A clock
// time alone is the next time it comes around; a weekday is its next day.
func clockResetTime(m []string, now time.Time) (time.Time, bool) {
	weekday, month, day, year, hourText, minuteText, half, zone := m[1], m[2], m[3], m[4], m[5], m[6], strings.ToLower(m[7]), m[8]
	if minuteText == "" && half == "" {
		return time.Time{}, false // a bare number is not a clock time
	}
	hour, _ := strconv.Atoi(hourText)
	minute, _ := strconv.Atoi(minuteText)
	if minute > 59 || hour > 23 || (half != "" && (hour < 1 || hour > 12)) {
		return time.Time{}, false
	}
	switch {
	case half == "a" && hour == 12:
		hour = 0
	case half == "p" && hour < 12:
		hour += 12
	}
	loc := now.Location()
	if zone != "" {
		if l, err := time.LoadLocation(zone); err == nil {
			loc = l
		}
	}
	local := now.In(loc)
	y, mo, d := local.Date()
	if month != "" {
		mo = resetMonths[strings.ToLower(month[:3])]
		d, _ = strconv.Atoi(day)
		if year != "" {
			y, _ = strconv.Atoi(year)
		}
	}
	t := time.Date(y, mo, d, hour, minute, 0, 0, loc)
	switch {
	case month != "":
		// A date without a year that is long past means next year.
		if year == "" && t.Before(now.Add(-24*time.Hour)) {
			t = t.AddDate(1, 0, 0)
		}
	case weekday != "":
		want := resetWeekdays[strings.ToLower(weekday[:3])]
		for range 8 {
			if t.Weekday() == want && t.After(now) {
				break
			}
			t = t.AddDate(0, 0, 1)
		}
	default:
		// Only roll to tomorrow when the time is well past, so a reset
		// named a few seconds late isn't pushed back a whole day.
		if t.Before(now.Add(-time.Hour)) {
			t = t.AddDate(0, 0, 1)
		}
	}
	return t, true
}

// limitRetryNote is the line the error box adds about the planned retry.
func limitRetryNote(at time.Time) string {
	return "Crush will send \"" + limitRetryPrompt + "\" at " + at.Local().Format("3:04 PM") + " when the limit resets."
}

// limitRetries holds each chat's planned retry after a usage limit.
type limitRetries struct {
	mu      sync.Mutex
	pending map[string]chan struct{} // closed to drop the retry

	// Test hooks; zero values use the real clock.
	now  func() time.Time
	poll time.Duration
}

// plan sends a chat its retry at the given time unless it is dropped
// first. A newer plan for the chat replaces the older one.
func (r *limitRetries) plan(sessionID string, at time.Time, send func()) {
	stop := make(chan struct{})
	r.mu.Lock()
	if r.pending == nil {
		r.pending = map[string]chan struct{}{}
	}
	if old := r.pending[sessionID]; old != nil {
		close(old)
	}
	r.pending[sessionID] = stop
	now, poll := r.now, r.poll
	r.mu.Unlock()
	if now == nil {
		now = time.Now
	}
	if poll <= 0 {
		poll = limitRetryPoll
	}
	go func() {
		ticker := time.NewTicker(poll)
		defer ticker.Stop()
		// Round(0) drops the monotonic reading, so the wait follows the
		// wall clock across sleep.
		for now().Round(0).Before(at) {
			select {
			case <-stop:
				return
			case <-ticker.C:
			}
		}
		r.mu.Lock()
		if r.pending[sessionID] != stop {
			r.mu.Unlock()
			return // dropped or replaced as the time came
		}
		delete(r.pending, sessionID)
		r.mu.Unlock()
		send()
	}()
}

// drop cancels a chat's planned retry, if it has one.
func (r *limitRetries) drop(sessionID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if stop := r.pending[sessionID]; stop != nil {
		close(stop)
		delete(r.pending, sessionID)
	}
}

// planLimitRetry continues a chat whose turn failed on a usage limit that
// names its reset time. Only interactive chats do: helpers and headless
// runs report their failure to whoever started them instead.
func (c *coordinator) planLimitRetry(sessionID string, err error) {
	if !c.interactive {
		return
	}
	at, ok := limitRetryTime(err, time.Now())
	if !ok {
		return
	}
	slog.Info("Usage limit hit; continuing the chat when it resets", "session_id", sessionID, "at", at)
	c.limitRetries.plan(sessionID, at, func() {
		if c.tasks != nil && c.tasks.isClosing() {
			return
		}
		if _, err := c.Run(context.Background(), sessionID, limitRetryPrompt); err != nil && !errors.Is(err, context.Canceled) {
			slog.Warn("Continuing a chat after its usage limit reset failed", "session_id", sessionID, "error", err)
		}
	})
}

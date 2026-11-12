package cliagent

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/charmbracelet/crush/internal/lock"
	"github.com/google/uuid"
)

// Usage readings shared between engines (owner, 2026-11-12). Every open
// folder has its own engine, and each read every sign-in's usage on its
// own: five engines starting together asked Anthropic fifteen times in a
// few seconds and most reads were refused (429), so the meters said
// "can't read". An engine now first takes a reading another engine took
// moments ago, and only one engine at a time asks for a sign-in; a refusal
// is shared the same way, so the others do not ask again right after it.

// sharedLimitsFor is how long another engine's reading stands in for
// asking again.
const sharedLimitsFor = 30 * time.Second

// sharedLimitsOwner names this process in the readings it shares: an
// engine asks again rather than reuse its own.
var sharedLimitsOwner = uuid.NewString()

// sharedLimitsDir is the folder the readings are shared in,
// $XDG_STATE_HOME/crush/limits, or "" for no sharing. Tests share nothing
// unless they point it at a folder of their own.
var sharedLimitsDir = func() string {
	if testing.Testing() {
		return ""
	}
	state := os.Getenv("XDG_STATE_HOME")
	if state == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		state = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(state, "crush", "limits")
}

// freshLimitsKey marks a read that must ask the tool itself: a turn's own
// usage, or the numbers after a refill, are newer than any shared reading.
type freshLimitsKey struct{}

func freshLimits(ctx context.Context) context.Context {
	return context.WithValue(ctx, freshLimitsKey{}, true)
}

// sharedLimits is one account's reading, or its refusal, as an engine
// shares it.
type sharedLimits struct {
	Owner   string    `json:"owner"`
	ReadAt  time.Time `json:"read_at"`
	Limits  []Limit   `json:"limits,omitempty"`
	Refills int       `json:"refills"`
	Err     string    `json:"error,omitempty"`
}

// sharedLimitsPath is the file k's reading is shared in, or "".
func sharedLimitsPath(k limitKey) string {
	dir := sharedLimitsDir()
	if dir == "" {
		return ""
	}
	name := string(k.kind)
	if k.account != "" {
		name += "#" + k.account
	}
	return filepath.Join(dir, url.PathEscape(name)+".json")
}

// readSharedLimits is another engine's reading in path, when it is fresh
// and newer than this engine's own (newest).
func readSharedLimits(path string, newest time.Time) (sharedLimits, bool) {
	var r sharedLimits
	data, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(data, &r) != nil {
		return r, false
	}
	age := time.Since(r.ReadAt)
	if r.Owner == sharedLimitsOwner || age < 0 || age >= sharedLimitsFor || !r.ReadAt.After(newest) {
		return r, false
	}
	return r, len(r.Limits) > 0 || r.Err != ""
}

// writeSharedLimits shares a reading or a refusal; a read cut short by
// its own engine says nothing about the tool and is not shared.
func writeSharedLimits(ctx context.Context, path string, limits []Limit, refills int, err error) {
	r := sharedLimits{Owner: sharedLimitsOwner, ReadAt: time.Now(), Limits: limits, Refills: refills}
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		r.Limits, r.Err = nil, err.Error()
	} else if len(limits) == 0 {
		return
	}
	data, jsonErr := json.Marshal(r)
	if jsonErr != nil || os.MkdirAll(filepath.Dir(path), 0o700) != nil {
		return
	}
	tmp := path + "." + sharedLimitsOwner + ".tmp"
	if os.WriteFile(tmp, data, 0o600) != nil || os.Rename(tmp, path) != nil {
		_ = os.Remove(tmp)
	}
}

// sharedLimitsError is another engine's refusal, taken as this engine's.
type sharedLimitsError string

func (e sharedLimitsError) Error() string { return string(e) }

// readLimits is one reading of k and when it was taken: another engine's
// fresh one when there is one, else fetch's own, which is then shared. Only
// one engine at a time asks for k; the others wait for its reading. refills
// is where fetch reports the account's refills.
func readLimits(ctx context.Context, k limitKey, fetch func(context.Context) ([]Limit, error), refills *int) ([]Limit, time.Time, error) {
	path := sharedLimitsPath(k)
	if path == "" {
		limits, err := fetch(ctx)
		return limits, time.Now(), err
	}
	if ctx.Value(freshLimitsKey{}) == nil {
		limitStore.Lock()
		newest := limitStore.readAt[k]
		limitStore.Unlock()
		taken := func() (sharedLimits, bool) { return readSharedLimits(path, newest) }
		r, ok := taken()
		if !ok {
			// The wait is bounded: an engine stuck in its read must not
			// hold the others' meters.
			_ = os.MkdirAll(filepath.Dir(path), 0o700)
			wait, cancel := context.WithTimeout(ctx, sharedLimitsFor)
			unlock, err := lock.File(wait, path+".lock")
			cancel()
			if err == nil {
				defer unlock()
			}
			r, ok = taken()
		}
		if ok {
			if r.Err != "" {
				return nil, r.ReadAt, sharedLimitsError(r.Err)
			}
			*refills = r.Refills
			return r.Limits, r.ReadAt, nil
		}
	}
	limits, err := fetch(ctx)
	writeSharedLimits(ctx, path, limits, *refills, err)
	return limits, time.Now(), err
}

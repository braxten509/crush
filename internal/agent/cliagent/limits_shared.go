package cliagent

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
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

// sharedLimitsGap is the least time between asking two sign-ins of one
// tool (owner, 2026-11-12: "a delay between each"): they are asked one
// after another, never all at once.
var sharedLimitsGap = 3 * time.Second

// refusalWait is the least time no engine asks a sign-in again after its
// tool said "too many asks" (Anthropic's usage address allows a few reads,
// then refuses for a while, and asking on keeps it shut).
const refusalWait = 5 * time.Minute

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
	// ModelsAt is when the per-model limits (Claude's weekly Fable one)
	// were read: only the usage address knows them, and a turn's own
	// reading carries an older copy.
	ModelsAt time.Time `json:"models_at,omitzero"`
	// Until is set on a "too many asks" refusal: no engine asks again
	// before it.
	Until time.Time `json:"until,omitzero"`
}

// refusedError is a tool's "too many asks" answer (HTTP 429); after is how
// long it said to wait, when it said.
type refusedError struct {
	msg   string
	after time.Duration
}

func (e refusedError) Error() string { return e.msg }

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

// loadSharedLimits is the reading or refusal kept in path, if any.
func loadSharedLimits(path string) (sharedLimits, bool) {
	var r sharedLimits
	data, err := os.ReadFile(path)
	return r, err == nil && json.Unmarshal(data, &r) == nil
}

// heldRefusal is a "too many asks" refusal in path that still stands: no
// engine, this one included, asks before its time is up.
func heldRefusal(path string) (sharedLimits, bool) {
	r, ok := loadSharedLimits(path)
	return r, ok && r.Err != "" && time.Now().Before(r.Until)
}

// readSharedLimits is another engine's reading in path, when it is fresh
// and newer than this engine's own (newest).
func readSharedLimits(path string, newest time.Time) (sharedLimits, bool) {
	r, ok := loadSharedLimits(path)
	if !ok {
		return r, false
	}
	age := time.Since(r.ReadAt)
	// A reading as old as this engine's own is the one it already took
	// from the saved file (adoptSavedLimits): it still stands in.
	if r.Owner == sharedLimitsOwner || age < 0 || age >= sharedLimitsFor || r.ReadAt.Before(newest) {
		return r, false
	}
	return r, len(r.Limits) > 0 || r.Err != ""
}

// writeSharedLimits shares a reading or a refusal taken at at; a read cut
// short by its own engine says nothing about the tool and is not shared.
func writeSharedLimits(ctx context.Context, path string, limits []Limit, refills int, err error, at time.Time) {
	r := sharedLimits{Owner: sharedLimitsOwner, ReadAt: at, ModelsAt: at, Limits: limits, Refills: refills}
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		r.Limits, r.Err = nil, err.Error()
		if refused := (refusedError{}); errors.As(err, &refused) {
			r.Until = r.ReadAt.Add(max(refusalWait, refused.after))
		}
	} else if len(limits) == 0 {
		return
	}
	writeLimitsFile(path, r)
}

// writeLimitsFile puts r in path in one step, so a reader never sees half
// a file.
func writeLimitsFile(path string, r sharedLimits) {
	data, err := json.Marshal(r)
	if err != nil || os.MkdirAll(filepath.Dir(path), 0o700) != nil {
		return
	}
	tmp := path + "." + sharedLimitsOwner + ".tmp"
	if os.WriteFile(tmp, data, 0o600) != nil || os.Rename(tmp, path) != nil {
		_ = os.Remove(tmp)
	}
}

// Remembered readings (owner, 2026-11-12: "remember the last amount until
// the real one is found"). An engine starts with nothing read, so a
// refused first read left a sign-in's meters at "can't read". Every
// reading is now also kept in a file of its own, which no refusal
// overwrites, and an engine with nothing read for a sign-in shows that one
// until a reading of its own arrives.

// savedLimitsPath is the file k's last reading is remembered in, or "".
func savedLimitsPath(k limitKey) string {
	path := sharedLimitsPath(k)
	if path == "" {
		return ""
	}
	return strings.TrimSuffix(path, ".json") + ".last.json"
}

// Readings taken during a turn (owner, 2026-11-12: a working sign-in's
// meter showed 61% left when 45% was true). Claude Code reports a sign-in's
// usage with every reply, but only the engine running the turn heard it;
// the others, the window's own included, could only ask Anthropic's usage
// address, which mostly refuses. Every engine now saves each reading it
// gets in the remembered file, which only ever moves forward, and takes
// a newer one from there whenever it is asked for a refresh, before any
// ask of the tool.

// joinLimits is account's windows for the whole account with models'
// per-model ones.
func joinLimits(account, models []Limit) []Limit {
	out := slices.DeleteFunc(slices.Clone(account), func(l Limit) bool { return l.Model != "" })
	for _, l := range models {
		if l.Model != "" {
			out = append(out, l)
		}
	}
	return out
}

// mergeReadings is what is known of a sign-in after reading a and then
// hearing of b: b's account windows when b was taken later, b's per-model
// limits when they were read later (or a has none). changed is false when
// b adds nothing.
func mergeReadings(a, b sharedLimits) (merged sharedLimits, changed bool) {
	later := b.ReadAt.After(a.ReadAt)
	hasModel := func(l Limit) bool { return l.Model != "" }
	models := b.ModelsAt.After(a.ModelsAt) || (later && !slices.ContainsFunc(a.Limits, hasModel))
	if !later && !models {
		return a, false
	}
	merged, account, perModel := a, a.Limits, a.Limits
	if later {
		merged.ReadAt, merged.Refills, account = b.ReadAt, b.Refills, b.Limits
	}
	if models {
		merged.ModelsAt, perModel = b.ModelsAt, b.Limits
	}
	merged.Limits = joinLimits(account, perModel)
	return merged, true
}

// saveLimits remembers a reading of k taken at at, with per-model limits
// read at modelsAt. The file only moves forward: an older reading never
// replaces a newer one, though its newer per-model limits are kept. Engines
// save under a lock, so two at once cannot undo each other.
func saveLimits(k limitKey, limits []Limit, refills int, at, modelsAt time.Time) {
	path := savedLimitsPath(k)
	if path == "" || len(limits) == 0 {
		return
	}
	// Bounded: a stuck engine must not hold a turn. Without the lock the
	// save goes ahead; the next reading puts any lost one right.
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	wait, cancel := context.WithTimeout(context.Background(), time.Second)
	unlock, err := lock.File(wait, path+".lock")
	cancel()
	if err == nil {
		defer unlock()
	}
	saved, _ := loadSharedLimits(path)
	merged, changed := mergeReadings(saved, sharedLimits{ReadAt: at, ModelsAt: modelsAt, Limits: limits, Refills: refills})
	if changed {
		merged.Owner, merged.Err, merged.Until = sharedLimitsOwner, "", time.Time{}
		writeLimitsFile(path, merged)
	}
}

// adoptSavedLimits takes k's remembered reading into the store when it is
// newer than the store's: another engine's turn may have read it moments
// ago. A reading taken within limitRefresh counts as this engine's own, so
// the next ask of the tool waits as long as after one; an older one (an
// earlier run's) only stands in until a real one arrives.
func adoptSavedLimits(k limitKey) {
	path := savedLimitsPath(k)
	if path == "" {
		return
	}
	r, ok := loadSharedLimits(path)
	if !ok || len(r.Limits) == 0 || r.ReadAt.IsZero() {
		return
	}
	limitStore.Lock()
	own := sharedLimits{ReadAt: limitStore.readAt[k], ModelsAt: limitStore.modelsAt[k], Limits: limitStore.byKind[k], Refills: -1}
	merged, changed := mergeReadings(own, r)
	if !changed {
		limitStore.Unlock()
		return
	}
	for _, m := range []*map[limitKey]time.Time{&limitStore.readAt, &limitStore.modelsAt, &limitStore.fetched} {
		if *m == nil {
			*m = map[limitKey]time.Time{}
		}
	}
	if limitStore.byKind == nil {
		limitStore.byKind = map[limitKey][]Limit{}
	}
	if limitStore.refills == nil {
		limitStore.refills = map[limitKey]int{}
	}
	if limitStore.seeded == nil {
		limitStore.seeded = map[limitKey]bool{}
	}
	limitStore.byKind[k] = merged.Limits
	limitStore.modelsAt[k] = merged.ModelsAt
	if r.ReadAt.After(own.ReadAt) {
		limitStore.readAt[k] = r.ReadAt
		if r.Refills >= 0 {
			limitStore.refills[k] = r.Refills
		}
		stale := time.Since(r.ReadAt) >= limitRefresh
		limitStore.seeded[k] = stale
		if !stale {
			// As new as an ask of this engine's own: the numbers are not
			// old, whatever the last ask said.
			delete(limitStore.lastErr, k)
			if r.ReadAt.After(limitStore.fetched[k]) {
				limitStore.fetched[k] = r.ReadAt
			}
		}
	}
	limitStore.Unlock()
	slog.Debug("Usage reading taken from the saved file", "kind", k.kind, "account", k.account, "read_at", r.ReadAt, "models_at", r.ModelsAt)
	limitsChanged(k)
}

// waitSignInGap holds an ask for k until sharedLimitsGap has passed since
// another sign-in of the same tool was asked, by any engine, and notes
// this ask. Asking the same sign-in again waits for nothing.
func waitSignInGap(ctx context.Context, k limitKey) {
	dir := sharedLimitsDir()
	if dir == "" {
		return
	}
	base := filepath.Join(dir, url.PathEscape(string(k.kind))+".asked")
	// Bounded like the reading's own lock: a stuck engine holds no one.
	wait, cancel := context.WithTimeout(ctx, sharedLimitsFor)
	unlock, err := lock.File(wait, base+".lock")
	cancel()
	if err != nil {
		return
	}
	defer unlock()
	type asked struct {
		Account string    `json:"account"`
		At      time.Time `json:"at"`
	}
	var last asked
	if data, err := os.ReadFile(base + ".json"); err == nil && json.Unmarshal(data, &last) == nil && last.Account != k.account {
		if left := sharedLimitsGap - time.Since(last.At); left > 0 && left <= sharedLimitsGap {
			select {
			case <-ctx.Done():
			case <-time.After(left):
			}
		}
	}
	if data, err := json.Marshal(asked{Account: k.account, At: time.Now()}); err == nil {
		_ = os.WriteFile(base+".json", data, 0o600)
	}
}

// takeModelLimits reports that some engine asked for k's usage within
// limitRefresh, so a working chat of this one does not: all engines
// together ask that often, not each (five engines with a working Claude
// chat asked five times as often and were refused). Another engine's
// per-model limits are taken from its reading; its account windows are
// older than the turn's own and are left.
func takeModelLimits(k limitKey) bool {
	path := sharedLimitsPath(k)
	if path == "" {
		return false
	}
	r, ok := loadSharedLimits(path)
	if age := time.Since(r.ReadAt); !ok || age < 0 || age >= limitRefresh {
		return false
	}
	limitStore.Lock()
	if limitStore.asked == nil {
		limitStore.asked = map[limitKey]time.Time{}
	}
	limitStore.asked[k] = r.ReadAt
	own := limitStore.byKind[k]
	changed := r.Owner != sharedLimitsOwner && len(r.Limits) > 0 && len(own) > 0
	if changed {
		merged := slices.DeleteFunc(slices.Clone(own), func(l Limit) bool { return l.Model != "" })
		for _, l := range r.Limits {
			if l.Model != "" {
				merged = append(merged, l)
			}
		}
		limitStore.byKind[k] = merged
		if limitStore.modelsAt == nil {
			limitStore.modelsAt = map[limitKey]time.Time{}
		}
		limitStore.modelsAt[k] = r.ReadAt
	}
	limitStore.Unlock()
	if changed {
		limitsChanged(k)
	}
	return true
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
	if r, held := heldRefusal(path); held {
		return nil, r.ReadAt, sharedLimitsError(r.Err)
	}
	if ctx.Value(freshLimitsKey{}) == nil {
		limitStore.Lock()
		newest := limitStore.readAt[k]
		if limitStore.seeded[k] {
			// Only remembered: any engine's fresh reading is newer news.
			newest = time.Time{}
		}
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
	waitSignInGap(ctx, k)
	limits, err := fetch(ctx)
	at := time.Now()
	writeSharedLimits(ctx, path, limits, *refills, err, at)
	return limits, at, err
}

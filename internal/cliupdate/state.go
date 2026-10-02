package cliupdate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// AutoCheckEvery spaces out the startup checks so opening many Crush windows
// doesn't hit GitHub's unauthenticated rate limit.
var AutoCheckEvery = time.Hour

type state struct {
	CheckedAt time.Time `json:"checked_at"`
	// Declined maps a CLI binary to the version the user said no to; it
	// isn't offered again at startup until a newer one comes out.
	Declined map[string]string `json:"declined,omitempty"`
}

var statePath = func() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "crush", "cli-updates.json")
}

func loadState() state {
	var s state
	if p := statePath(); p != "" {
		if data, err := os.ReadFile(p); err == nil {
			_ = json.Unmarshal(data, &s)
		}
	}
	if s.Declined == nil {
		s.Declined = map[string]string{}
	}
	return s
}

func saveState(s state) {
	p := statePath()
	if p == "" {
		return
	}
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	if data, err := json.MarshalIndent(s, "", "  "); err == nil {
		_ = os.WriteFile(p, data, 0o644)
	}
}

// StartAutoCheck reports whether a startup check is due and, if so, records
// it as started.
func StartAutoCheck() bool {
	s := loadState()
	if time.Since(s.CheckedAt) < AutoCheckEvery {
		return false
	}
	s.CheckedAt = time.Now()
	saveState(s)
	return true
}

// WithoutDeclined drops updates whose version the user already declined.
func WithoutDeclined(updates []Update) []Update {
	declined := loadState().Declined
	var out []Update
	for _, u := range updates {
		if v, ok := declined[u.Bin]; ok && !Newer(u.Latest, v) {
			continue
		}
		out = append(out, u)
	}
	return out
}

// Decline remembers that the user said no to these versions.
func Decline(updates []Update) {
	s := loadState()
	for _, u := range updates {
		s.Declined[u.Bin] = u.Latest
	}
	saveState(s)
}

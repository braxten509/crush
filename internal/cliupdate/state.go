package cliupdate

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/charmbracelet/crush/internal/lock"
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
	return loadStateFile(statePath())
}

func loadStateFile(p string) state {
	var s state
	if p != "" {
		if data, err := os.ReadFile(p); err == nil {
			_ = json.Unmarshal(data, &s)
		}
	}
	if s.Declined == nil {
		s.Declined = map[string]string{}
	}
	return s
}

func saveState(p string, s state) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(p), filepath.Base(p)+".*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), p)
}

// updateState holds a separate, persistent lock file across the entire
// read-modify-write, including the atomic replacement of the state file.
func updateState(change func(*state) bool) error {
	p := statePath()
	if p == "" {
		return os.ErrNotExist
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	release, err := lock.File(ctx, p+".lock")
	if err != nil {
		return err
	}
	defer release()
	s := loadStateFile(p)
	if !change(&s) {
		return nil
	}
	return saveState(p, s)
}

// StartAutoCheck reports whether a startup check is due and, if so, records
// it as started.
func StartAutoCheck() bool {
	var due bool
	if err := updateState(func(s *state) bool {
		due = time.Since(s.CheckedAt) >= AutoCheckEvery
		if due {
			s.CheckedAt = time.Now()
		}
		return due
	}); err != nil {
		slog.Debug("Record CLI update check failed", "error", err)
		return false
	}
	return due
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
	if err := updateState(func(s *state) bool {
		for _, u := range updates {
			s.Declined[u.Bin] = u.Latest
		}
		return true
	}); err != nil {
		slog.Debug("Record declined CLI updates failed", "error", err)
	}
}

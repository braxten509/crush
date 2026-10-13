package sessionhost

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// prefs is what the host remembers between runs.
type prefs struct {
	// ListClosed: the session list was shrunk to the strip. Stored as
	// "closed" so a missing file means open.
	ListClosed bool `json:"list_closed"`
}

// prefsPath is $XDG_STATE_HOME/crush/session-list.json, or
// ~/.local/state/crush/session-list.json, on macOS and Linux alike.
func prefsPath() (string, error) {
	state := os.Getenv("XDG_STATE_HOME")
	if state == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		state = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(state, "crush", "session-list.json"), nil
}

func loadPrefs(path string) prefs {
	var p prefs
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &p)
	}
	return p
}

// savePrefs writes p through a temporary file, so a crash mid-write never
// leaves a broken file behind.
func savePrefs(path string, p prefs) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".session-list-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path)
}

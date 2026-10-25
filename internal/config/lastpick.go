package config

import (
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
)

// The last pick is the whole model choice (effort, account, FAST, thinking)
// the owner picked most recently, in any chat or with no chat open. A new
// chat starts on it and then owns its model; no chat ever runs on the last
// pick itself (owner, 2026-10-24: no shared model anywhere). It lives in its
// own file, read fresh each time, so every running engine sees the newest
// pick.

// LastPickFile is where the last pick is kept.
func LastPickFile() string {
	return filepath.Join(GlobalWorkspaceDir(), "last-pick.json")
}

// ReadLastPick reads the last pick; ok is false when there is none.
func ReadLastPick() (SelectedModel, bool) {
	data, err := os.ReadFile(LastPickFile())
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			slog.Warn("Could not read the last picked model", "error", err)
		}
		return SelectedModel{}, false
	}
	var m SelectedModel
	if err := json.Unmarshal(data, &m); err != nil || m.Provider == "" || m.Model == "" {
		slog.Warn("The last picked model file is not readable", "error", err)
		return SelectedModel{}, false
	}
	return m, true
}

// WriteLastPick saves m as the last pick. The file is replaced whole, so a
// reader never sees half of it.
func WriteLastPick(m SelectedModel) error {
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	path := LastPickFile()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".last-pick-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// NewChatModel is the model a new chat starts on: the last pick, when this
// folder's config offers it. With no pick yet, the starting model seeds it
// once; a folder that can't run the pick starts on its own starting model
// and leaves the pick for the others.
func NewChatModel(cfg *Config) (SelectedModel, bool) {
	pick, picked := ReadLastPick()
	if cfg == nil {
		return pick, picked
	}
	if picked && cfg.IsModelAvailable(pick.Provider, pick.Model) {
		return pick, true
	}
	start, ok := cfg.StartModel()
	if !ok {
		return SelectedModel{}, false
	}
	if !picked {
		if err := WriteLastPick(start); err != nil {
			slog.Warn("Could not save the starting model as the last pick", "error", err)
		}
	}
	return start, true
}

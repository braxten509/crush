// Package prompthistory keeps the prompts the user sent, from every folder
// and every Crush app (terminal and desktop), in one shared file. ↑ in the
// editor walks it, newest first.
package prompthistory

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/jsonfile"
)

// Limit is how many prompts the file keeps.
const Limit = 100

// Path is the shared file, beside the global data file.
func Path() string {
	return filepath.Join(filepath.Dir(config.GlobalConfigData()), "prompt-history.json")
}

// shared reports whether the real file may be used: tests that don't
// point CRUSH_GLOBAL_DATA elsewhere never touch the user's history.
func shared() bool {
	return !testing.Testing() || os.Getenv("CRUSH_GLOBAL_DATA") != ""
}

// Entry is one saved prompt. Chat is the chat it was sent in, so locking
// that chat takes its prompts out (owner, 2026-11-03: locked chats); older
// entries have none and stay. An entry without a chat is saved as a plain
// string, as the file always held.
type Entry struct {
	Text string `json:"text"`
	Chat string `json:"chat,omitempty"`
}

func (e Entry) MarshalJSON() ([]byte, error) {
	if e.Chat == "" {
		return json.Marshal(e.Text)
	}
	type plain Entry
	return json.Marshal(plain(e))
}

func (e *Entry) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		*e = Entry{Text: text}
		return nil
	}
	type plain Entry
	var p plain
	if err := json.Unmarshal(data, &p); err != nil {
		return err
	}
	*e = Entry(p)
	return nil
}

// Add saves a sent prompt (shell commands as "!command"). Blank ones are
// skipped.
func Add(text string) {
	AddFor(text, "")
}

// AddFor saves a prompt sent in chat.
func AddFor(text, chat string) {
	if !shared() {
		return
	}
	if err := addEntry(Path(), Entry{Text: text, Chat: chat}); err != nil {
		slog.Warn("Failed to save prompt history", "error", err)
	}
}

// List returns the saved prompts, newest first.
func List() []string {
	if !shared() {
		return nil
	}
	return list(Path())
}

// The file holds the prompts oldest first.
func add(path, text string) error { return addEntry(path, Entry{Text: text}) }

func addEntry(path string, entry Entry) error {
	if strings.TrimSpace(entry.Text) == "" {
		return nil
	}
	return jsonfile.Update(path, func(saved *[]Entry) bool {
		*saved = append(*saved, entry)
		if extra := len(*saved) - Limit; extra > 0 {
			*saved = (*saved)[extra:]
		}
		return true
	})
}

func list(path string) []string {
	saved := jsonfile.Read[[]Entry](path)
	out := make([]string, 0, len(saved))
	for i := len(saved) - 1; i >= 0; i-- {
		out = append(out, saved[i].Text)
	}
	return out
}

// TakeChats removes the prompts sent in the given chats and returns them,
// oldest first, so a locked chat's prompts can go into the vault. A saved
// prompt without a chat stays: it can't be matched safely.
func TakeChats(path string, chats []string) ([]Entry, error) {
	var taken []Entry
	err := jsonfile.Update(path, func(saved *[]Entry) bool {
		kept := (*saved)[:0:0]
		for _, e := range *saved {
			if e.Chat != "" && slices.Contains(chats, e.Chat) {
				taken = append(taken, e)
			} else {
				kept = append(kept, e)
			}
		}
		if len(taken) == 0 {
			return false
		}
		*saved = kept
		return true
	})
	return taken, err
}

// Entries returns the prompts sent in the given chats, oldest first.
func Entries(path string, chats []string) []Entry {
	var out []Entry
	for _, e := range jsonfile.Read[[]Entry](path) {
		if e.Chat != "" && slices.Contains(chats, e.Chat) {
			out = append(out, e)
		}
	}
	return out
}

// PutBack adds prompts taken by TakeChats again, oldest first, before the
// newest Limit are kept; prompts there already are not added twice.
func PutBack(path string, entries []Entry) error {
	if len(entries) == 0 {
		return nil
	}
	return jsonfile.Update(path, func(saved *[]Entry) bool {
		for _, e := range entries {
			if !slices.Contains(*saved, e) {
				*saved = append(*saved, e)
			}
		}
		if extra := len(*saved) - Limit; extra > 0 {
			*saved = (*saved)[extra:]
		}
		return true
	})
}

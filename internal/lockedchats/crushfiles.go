package lockedchats

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strings"

	"github.com/charmbracelet/crush/internal/jsonfile"
	"github.com/charmbracelet/crush/internal/prompthistory"
)

// Crush's own shared files hold a few entries of every chat:
// cli-sessions.json (a chat's link to each CLI conversation, keyed
// "<chat>/<cli>"), task-history.json (its sub-agents) and the global
// prompt-history.json (prompts sent in it). Locking moves those entries.

// updateJSON changes a jsonfile-kept file, then overwrites the replaced
// copy's blocks so the removed entries are not left readable.
func updateJSON[T any](path string, change func(*T) bool) error {
	old, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		old = nil
	} else if err != nil {
		return err
	}
	err = jsonfile.Update(path, change)
	if old != nil {
		if err == nil {
			err = overwriteReplaced(old, path)
		}
		old.Close()
	}
	return err
}

// linkOwner is the chat a cli-sessions.json key belongs to.
func linkOwner(key string) string {
	chat, _, _ := strings.Cut(key, "/")
	return chat
}

func copyCrushEntry(r entryRef, dst string) error {
	var data any
	switch r.Type {
	case "crush-links":
		links := map[string]json.RawMessage{}
		for key, link := range jsonfile.Read[map[string]json.RawMessage](r.Path) {
			if slices.Contains(r.IDs, linkOwner(key)) {
				links[key] = link
			}
		}
		data = links
	case "crush-tasks":
		var tasks []json.RawMessage
		for _, task := range jsonfile.Read[[]json.RawMessage](r.Path) {
			if taskOwned(task, r.IDs) {
				tasks = append(tasks, task)
			}
		}
		data = tasks
	case "crush-prompts":
		data = prompthistory.Entries(r.Path, r.IDs)
	default:
		return fmt.Errorf("unknown entry type %q", r.Type)
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return writeFileSync(dst, encoded)
}

// taskOwned reports whether a saved sub-agent belongs to one of the chats.
func taskOwned(raw json.RawMessage, chats []string) bool {
	var task struct {
		SessionID string `json:"session_id"`
		ChildID   string `json:"child_id"`
	}
	if json.Unmarshal(raw, &task) != nil {
		return false
	}
	return slices.Contains(chats, task.SessionID) || slices.Contains(chats, task.ChildID)
}

func removeCrushEntry(r entryRef) error {
	switch r.Type {
	case "crush-links":
		return updateJSON(r.Path, func(links *map[string]json.RawMessage) bool {
			changed := false
			for key := range *links {
				if slices.Contains(r.IDs, linkOwner(key)) {
					delete(*links, key)
					changed = true
				}
			}
			return changed
		})
	case "crush-tasks":
		return updateJSON(r.Path, func(tasks *[]json.RawMessage) bool {
			before := len(*tasks)
			*tasks = slices.DeleteFunc(*tasks, func(task json.RawMessage) bool { return taskOwned(task, r.IDs) })
			return len(*tasks) != before
		})
	case "crush-prompts":
		old, err := os.Open(r.Path)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		} else if err != nil {
			return err
		}
		defer old.Close()
		taken, err := prompthistory.TakeChats(r.Path, r.IDs)
		if err != nil || len(taken) == 0 {
			return err
		}
		return overwriteReplaced(old, r.Path)
	}
	return fmt.Errorf("unknown entry type %q", r.Type)
}

func restoreCrushEntry(r entryRef, src string) error {
	saved, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	switch r.Type {
	case "crush-links":
		var links map[string]json.RawMessage
		if err := json.Unmarshal(saved, &links); err != nil {
			return err
		}
		if len(links) == 0 {
			return nil
		}
		return jsonfile.Update(r.Path, func(current *map[string]json.RawMessage) bool {
			if *current == nil {
				*current = map[string]json.RawMessage{}
			}
			for key, link := range links {
				if _, ok := (*current)[key]; !ok {
					(*current)[key] = link
				}
			}
			return true
		})
	case "crush-tasks":
		var tasks []json.RawMessage
		if err := json.Unmarshal(saved, &tasks); err != nil {
			return err
		}
		if len(tasks) == 0 {
			return nil
		}
		return jsonfile.Update(r.Path, func(current *[]json.RawMessage) bool {
			*current = append(*current, tasks...)
			return true
		})
	case "crush-prompts":
		var entries []prompthistory.Entry
		if err := json.Unmarshal(saved, &entries); err != nil {
			return err
		}
		return prompthistory.PutBack(r.Path, entries)
	}
	return fmt.Errorf("unknown entry type %q", r.Type)
}

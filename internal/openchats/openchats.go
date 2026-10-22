// Package openchats keeps the list of open chats the terminal's session
// list and Crush Desktop share (owner, 2026-10-20: "the two should be in
// sync": a chat opened in either shows in both, and closing it in either
// closes it in both). The list lives in one small file,
// $XDG_STATE_HOME/crush/open-chats.json, with the apps that have it open.
// The chats stay while any of those apps runs; once the last one is closed
// (or found gone, after a crash) the list empties, so the next app starts
// with none.
//
// The desktop's helper (desktop/src-tauri/src/openchats.rs) reads and
// writes the same file the same way: every change takes the lock file
// beside it, reads, changes and writes through a temporary file.
package openchats

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"time"
)

// Chat is one open chat.
type Chat struct {
	ID    string `json:"id"`
	Dir   string `json:"dir"`
	Title string `json:"title,omitempty"`
	// Opened is when it was first listed (Unix seconds); the list keeps
	// that order.
	Opened int64 `json:"opened"`
}

// App is a running program that has the list open.
type App struct {
	PID int `json:"pid"`
	// Start is the process's start time as the system reports it, so a
	// reused PID is not taken for the app.
	Start string `json:"start,omitempty"`
	Kind  string `json:"kind"`
	// Dirs are the folders whose chats the desktop hears and rings for
	// (owner, 2026-10-20: "only ring ONCE - from the desktop one").
	Dirs []string `json:"dirs,omitempty"`
}

type file struct {
	Version int    `json:"version"`
	Apps    []App  `json:"apps"`
	Chats   []Chat `json:"chats"`
}

// Path is $XDG_STATE_HOME/crush/open-chats.json, or
// ~/.local/state/crush/open-chats.json.
func Path() (string, error) {
	state := os.Getenv("XDG_STATE_HOME")
	if state == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		state = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(state, "crush", "open-chats.json"), nil
}

// List is the file at path.
type List struct{ path string }

// Open returns the shared list at Path.
func Open() (List, error) {
	path, err := Path()
	return List{path: path}, err
}

// At returns the list kept at path (tests).
func At(path string) List { return List{path: path} }

// Chats returns the open chats, oldest first; none when no app that has
// the list open is still running.
func (l List) Chats() []Chat {
	f := l.read()
	if len(liveApps(f.Apps)) == 0 {
		return nil
	}
	return f.Chats
}

// DesktopHears reports whether a running desktop app rings for the chats
// in dir, so a terminal there stays quiet and the desktop rings once.
func (l List) DesktopHears(dir string) bool {
	want := sameDir(dir)
	for _, a := range liveApps(l.read().Apps) {
		if a.Kind != "desktop" {
			continue
		}
		for _, d := range a.Dirs {
			if sameDir(d) == want {
				return true
			}
		}
	}
	return false
}

// DesktopOpen reports whether a desktop app is running, whatever folders
// it shows (agents may then use what only the desktop draws).
func (l List) DesktopOpen() bool {
	for _, a := range liveApps(l.read().Apps) {
		if a.Kind == "desktop" {
			return true
		}
	}
	return false
}

// AppOpen reports whether any app (desktop or terminal) is running.
func (l List) AppOpen() bool {
	return len(liveApps(l.read().Apps)) > 0
}

// sameDir is dir cleaned with its links followed, for comparing folders.
func sameDir(dir string) string {
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		return real
	}
	return filepath.Clean(dir)
}

// Join signs this process in as kind. When no other app has the list open
// it starts empty: both apps were closed. leave signs it out again, and
// empties the list when it was the last app.
func (l List) Join(kind string) (leave func(), err error) {
	me := App{PID: os.Getpid(), Start: processStart(os.Getpid()), Kind: kind}
	err = l.change(func(f *file) {
		f.Apps = liveApps(f.Apps)
		if len(f.Apps) == 0 {
			f.Chats = nil
		}
		f.Apps = append(slices.DeleteFunc(f.Apps, func(a App) bool { return a.PID == me.PID }), me)
	})
	if err != nil {
		return func() {}, err
	}
	return func() {
		_ = l.change(func(f *file) {
			f.Apps = slices.DeleteFunc(liveApps(f.Apps), func(a App) bool { return a.PID == me.PID })
			if len(f.Apps) == 0 {
				f.Chats = nil
			}
		})
	}, nil
}

// Add lists c at the end, or updates its folder and title when listed.
func (l List) Add(c Chat) error {
	if c.ID == "" {
		return nil
	}
	return l.change(func(f *file) { add(f, c) })
}

// Remove takes the chat with id off the list.
func (l List) Remove(id string) error {
	return l.change(func(f *file) {
		f.Chats = slices.DeleteFunc(f.Chats, func(c Chat) bool { return c.ID == id })
	})
}

// Swap puts c in the place of the chat with id old (a terminal row that
// moved to another chat), or adds it when old is not listed.
func (l List) Swap(old string, c Chat) error {
	if c.ID == "" {
		return nil
	}
	return l.change(func(f *file) {
		i := slices.IndexFunc(f.Chats, func(x Chat) bool { return x.ID == old })
		if i < 0 || old == c.ID {
			add(f, c)
			return
		}
		f.Chats = slices.DeleteFunc(f.Chats, func(x Chat) bool { return x.ID == c.ID })
		i = slices.IndexFunc(f.Chats, func(x Chat) bool { return x.ID == old })
		c.Opened = f.Chats[i].Opened
		f.Chats[i] = c
	})
}

func add(f *file, c Chat) {
	if i := slices.IndexFunc(f.Chats, func(x Chat) bool { return x.ID == c.ID }); i >= 0 {
		if c.Dir != "" {
			f.Chats[i].Dir = c.Dir
		}
		if c.Title != "" {
			f.Chats[i].Title = c.Title
		}
		return
	}
	if c.Opened == 0 {
		c.Opened = time.Now().Unix()
	}
	f.Chats = append(f.Chats, c)
}

func (l List) read() file {
	var f file
	if b, err := os.ReadFile(l.path); err == nil {
		_ = json.Unmarshal(b, &f)
	}
	return f
}

// change runs fn on the file under the lock and writes the result.
func (l List) change(fn func(*file)) error {
	if l.path == "" {
		return errors.New("no open-chats file")
	}
	dir := filepath.Dir(l.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	unlock, err := lock(l.path + ".lock")
	if err != nil {
		return err
	}
	defer unlock()
	f := l.read()
	fn(&f)
	f.Version = 1
	b, err := json.MarshalIndent(f, "", " ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".open-chats-*")
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
	return os.Rename(tmp.Name(), l.path)
}

// liveApps keeps the apps whose process still runs.
func liveApps(apps []App) []App {
	var out []App
	for _, a := range apps {
		if a.PID > 0 && alive(a.PID, a.Start) {
			out = append(out, a)
		}
	}
	return out
}

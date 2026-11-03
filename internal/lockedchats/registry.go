package lockedchats

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/charmbracelet/crush/internal/jsonfile"
	"github.com/charmbracelet/crush/internal/session"
)

// The registry lists the unlocked chats and the Crush that unlocked each
// (PLAN §3), so a crash is told apart from a chat another Crush still has
// open, and the vault closes only when no chat is unlocked.

type registryEntry struct {
	DataDir   string `json:"data_dir"`
	SessionID string `json:"session_id"`
	PID       int    `json:"pid"`
	Start     string `json:"start,omitempty"`
}

// registryPath is $XDG_RUNTIME_DIR/crush/unlocked-chats.json.
var registryPath = func() string {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = filepath.Join(os.TempDir(), "crush-"+strconv.Itoa(os.Getuid()))
	}
	return filepath.Join(dir, "crush", "unlocked-chats.json")
}

// processStart is field 22 of /proc/PID/stat, so a reused PID is not taken
// for the Crush that unlocked a chat.
func processStart(pid int) string {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return ""
	}
	// The command name in field 2 may hold spaces; count after its ")".
	rest := string(data)
	if i := strings.LastIndexByte(rest, ')'); i >= 0 {
		rest = rest[i+1:]
	}
	fields := strings.Fields(rest)
	if len(fields) < 20 {
		return ""
	}
	return fields[19]
}

func (e registryEntry) alive() bool {
	if e.PID <= 0 {
		return false
	}
	start := processStart(e.PID)
	return start != "" && (e.Start == "" || e.Start == start)
}

func sameData(a, b string) bool {
	clean := func(p string) string {
		if real, err := filepath.EvalSymlinks(p); err == nil {
			return real
		}
		return filepath.Clean(p)
	}
	return clean(a) == clean(b)
}

// register records that this process unlocked the chat.
func register(dataDir, id string) error {
	me := registryEntry{DataDir: dataDir, SessionID: id, PID: os.Getpid(), Start: processStart(os.Getpid())}
	return jsonfile.Update(registryPath(), func(list *[]registryEntry) bool {
		*list = slices.DeleteFunc(*list, func(e registryEntry) bool {
			return !e.alive() || (e.SessionID == id && sameData(e.DataDir, dataDir))
		})
		*list = append(*list, me)
		return true
	})
}

// unregister drops the chat from the registry.
func unregister(dataDir, id string) error {
	return jsonfile.Update(registryPath(), func(list *[]registryEntry) bool {
		before := len(*list)
		*list = slices.DeleteFunc(*list, func(e registryEntry) bool {
			return e.SessionID == id && sameData(e.DataDir, dataDir)
		})
		return len(*list) != before
	})
}

// liveOwner reports whether a running Crush other than this one has the
// chat unlocked.
func liveOwner(dataDir, id string) bool {
	for _, e := range jsonfile.Read[[]registryEntry](registryPath()) {
		if e.SessionID == id && sameData(e.DataDir, dataDir) && e.PID != os.Getpid() && e.alive() {
			return true
		}
	}
	return false
}

// anyUnlocked reports whether a live Crush has any chat unlocked.
func anyUnlocked() bool {
	for _, e := range jsonfile.Read[[]registryEntry](registryPath()) {
		if e.alive() {
			return true
		}
	}
	return false
}

// ownedHere lists the chats of dataDir this process unlocked.
func ownedHere(dataDir string) []string {
	var ids []string
	for _, e := range jsonfile.Read[[]registryEntry](registryPath()) {
		if e.PID == os.Getpid() && sameData(e.DataDir, dataDir) {
			ids = append(ids, e.SessionID)
		}
	}
	return ids
}

// Inherit makes a fork, clone or branch of an unlocked locked chat a
// locked chat too, unlocked for now, so it goes into the vault with the
// rest (PLAN §2: forks of a locked chat start unlocked). A copy of a normal
// chat stays normal.
func Inherit(ctx context.Context, sessions session.Service, dataDir, source, copy string) error {
	if sessions == nil || copy == "" {
		return nil
	}
	src, err := sessions.Get(ctx, source)
	if err != nil || src.LockState != session.LockStateUnlocked {
		return err
	}
	if err := register(dataDir, copy); err != nil {
		return err
	}
	_, err = sessions.SetLockState(ctx, copy, session.LockStateUnlocked, "")
	return err
}

// Register records that this Crush has the chat unlocked.
func Register(dataDir, id string) error { return register(dataDir, id) }

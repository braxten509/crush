package agent

// Each chat gets a private scratch folder, ~/.cache/crush-chats/<chat id>,
// shared by its helpers (they find it through their parent chats) and kept
// across model switches. secretguard refuses tool calls that name another
// chat's folder. At most chatFolderLimit folders are kept; the least
// recently used go first (owner, 2026-11-10).

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/crush/internal/secretguard"
	"github.com/charmbracelet/crush/internal/session"
)

// chatFolderLimit is how many chat folders the cache folder may hold.
const chatFolderLimit = 50

// chatFoldersRoot is where chat folders live. Tests get none unless they
// set it, so they never touch the real cache folder.
var chatFoldersRoot = func() string {
	if testing.Testing() {
		return ""
	}
	return secretguard.ChatFoldersRoot()
}

// topChatID follows a session's parent chats up to the chat the user
// opened.
func topChatID(ctx context.Context, sessions session.Service, sessionID string) string {
	id := sessionID
	if sessions == nil {
		return id
	}
	for range 16 {
		s, err := sessions.Get(ctx, id)
		if err != nil || s.ParentSessionID == "" {
			return id
		}
		id = s.ParentSessionID
	}
	return id
}

// chatFolder makes (or marks as just used) the private folder of the chat
// sessionID belongs to, trims the folder list, and returns the folder, or
// "" when it can't be made.
func chatFolder(ctx context.Context, sessions session.Service, sessionID string) string {
	if sessionID == "" {
		return ""
	}
	id := filepath.Base(topChatID(ctx, sessions, sessionID))
	if id == "." || id == "/" || id == ".." {
		return ""
	}
	root := chatFoldersRoot()
	if root == "" {
		return ""
	}
	dir := filepath.Join(root, id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		slog.Warn("Could not make the chat folder", "dir", dir, "error", err)
		return ""
	}
	now := time.Now()
	_ = os.Chtimes(dir, now, now)
	trimChatFolders(root, id, chatFolderLimit)
	return dir
}

// trimChatFolders deletes the least recently used chat folders in root,
// never keep, until at most limit are left.
func trimChatFolders(root, keep string, limit int) {
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) <= limit {
		return
	}
	type folder struct {
		name string
		used time.Time
	}
	var folders []folder
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil || !entry.IsDir() || entry.Name() == keep {
			continue
		}
		folders = append(folders, folder{entry.Name(), info.ModTime()})
	}
	sort.Slice(folders, func(i, j int) bool { return folders[i].used.Before(folders[j].used) })
	extra := len(entries) - limit
	for _, f := range folders[:min(extra, len(folders))] {
		if err := os.RemoveAll(filepath.Join(root, f.name)); err != nil {
			slog.Warn("Could not delete an old chat folder", "name", f.name, "error", err)
		}
	}
}

// chatFolderNote tells an agent where its chat's scratch folder is.
func chatFolderNote(dir string) string {
	if dir == "" {
		return ""
	}
	return fmt.Sprintf(`<crush_chat_folder>
This chat's private scratch folder is %s. Put all scratch work you make
(sketches, pictures, downloads, notes, temporary files) there instead of
elsewhere in ~/.cache. Helpers this chat starts share it. Other chats'
folders next to it are off limits: Crush refuses commands that name them.
Only the 50 most recently used chat folders are kept, so keep anything worth
saving in the project instead.
</crush_chat_folder>`, dir)
}

// envValue is the value of key in a list of KEY=value entries.
func envValue(env []string, key string) string {
	for _, variable := range env {
		if value, ok := strings.CutPrefix(variable, key+"="); ok {
			return value
		}
	}
	return ""
}

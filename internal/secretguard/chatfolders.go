package secretguard

// Each Crush chat keeps its scratch work in its own folder under
// ~/.cache/crush-chats, and its helpers share it. Agents in different
// chats should not see each other's work (one copied another's sketch
// answers, owner 2026-11-10), so tool calls that name another chat's
// folder are refused. Like the secrets check this is a word check, not a
// sandbox.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	// ChatFoldersName is the folder in the cache folder holding one folder
	// per chat.
	ChatFoldersName = "crush-chats"
	// ChatFolderEnv names the chat's own folder for agent processes and
	// the hooks they run.
	ChatFolderEnv = "CRUSH_CHAT_DIR"
	// ChatFolderReason is what the agent sees when a tool call is blocked.
	ChatFolderReason = "Blocked: this names another chat's private folder (or the list of them) in ~/.cache/crush-chats. Use only your own chat's folder, named in your instructions."
)

var (
	// A glob in the cache folder that could expand to crush-chats, like
	// ~/.cache/cr* or ~/.cache/*. The group is the literal start.
	cacheGlob = regexp.MustCompile(`\.cache/([^/\s"'*?\[{]*)[*?\[{]`)
	// The chat folder variable climbing out of its folder.
	envClimb = regexp.MustCompile(`CRUSH_CHAT_DIR\}?/\.\.`)
)

// ChatFoldersRoot is the folder holding every chat's folder.
func ChatFoldersRoot() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".cache")
	}
	return filepath.Join(dir, ChatFoldersName)
}

// BlocksChatFolder reports whether a tool call with this JSON input names
// a chat folder other than own (the full path of the caller's chat folder,
// or "" outside a chat, where every chat folder is off limits).
// Write and edit tools are checked by their target path only, so code and
// notes may mention the folder.
func BlocksChatFolder(tool string, input []byte, own string) bool {
	var value any
	if json.Unmarshal(input, &value) != nil {
		value = string(input)
	}
	if fields, ok := value.(map[string]any); ok && writesOnly(tool) {
		var paths []any
		for _, key := range []string{"file_path", "path", "notebook_path"} {
			if path, ok := fields[key].(string); ok {
				paths = append(paths, path)
			}
		}
		value = paths
	}
	text := strings.Join(stringsIn(value, nil), "\n")
	if envClimb.MatchString(text) {
		return true
	}
	for _, m := range cacheGlob.FindAllStringSubmatch(text, -1) {
		if strings.HasPrefix(ChatFoldersName, m[1]) {
			return true
		}
	}
	ownID := ""
	if own != "" {
		ownID = filepath.Base(own)
	}
	lower := strings.ToLower(text)
	for rest := lower; ; {
		i := strings.Index(rest, ChatFoldersName)
		if i < 0 {
			return false
		}
		rest = rest[i+len(ChatFoldersName):]
		if ownID == "" || !strings.HasPrefix(rest, "/"+strings.ToLower(ownID)) {
			return true
		}
		after := rest[1+len(ownID):]
		if after != "" && (isNameByte(after[0]) || strings.HasPrefix(after, "/..")) {
			return true
		}
	}
}

// isNameByte reports bytes that can continue a folder name, so own ID
// "abc" doesn't let "abcd" through.
func isNameByte(b byte) bool {
	return b == '-' || b == '_' || b == '.' ||
		b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}

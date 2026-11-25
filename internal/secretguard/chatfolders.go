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
	// Command text ends a path at whitespace or shell syntax; a write tool's
	// path fields are whole paths, so only the line between them ends one.
	stops := " \t\n\r\"';&|)><`"
	if fields, ok := value.(map[string]any); ok && writesOnly(tool) {
		stops = "\n"
		var paths []any
		for _, key := range []string{"file_path", "filePath", "target_file", "path", "absolute_path", "notebook_path", "Path", "TargetFile", "AbsolutePath"} {
			if path, ok := fields[key].(string); ok {
				paths = append(paths, path)
			}
		}
		value = paths
	}
	text := strings.Join(stringsIn(value, nil), "\n")
	if envClimbs(text, stops) {
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
		if after != "" && (isNameByte(after[0]) || hasClimbInToken(after, quotedStops(lower[:len(lower)-len(after)], stops))) {
			return true
		}
	}
}

func envClimbs(text, stops string) bool {
	for rest := text; ; {
		i := strings.Index(rest, ChatFolderEnv)
		if i < 0 {
			return false
		}
		rest = strings.TrimPrefix(rest[i+len(ChatFolderEnv):], "}")
		if hasClimbInToken(rest, quotedStops(text[:len(text)-len(rest)], stops)) {
			return true
		}
	}
}

// quotedStops narrows a command's path stops when the path sits inside an
// open quote: then only the closing quote (or a new line) ends it, so a
// quoted "sub dir/../../other" is read whole. Whole-path stops stay as given.
func quotedStops(before, stops string) string {
	if stops == "\n" {
		return stops
	}
	var open byte
	for i := 0; i < len(before); i++ {
		switch c := before[i]; {
		case c == '\\' && open != '\'':
			i++
		case (c == '\'' || c == '"') && open == 0:
			open = c
		case c == open:
			open = 0
		}
	}
	if open == 0 {
		return stops
	}
	return string(open) + "\n"
}

// hasClimbInToken reports whether the path suffix starting at s contains a
// ".." segment that climbs above the folder before the path token ends at whitespace or shell syntax.
func hasClimbInToken(s, stops string) bool {
	if strings.HasPrefix(s, `"/`) || strings.HasPrefix(s, `'/`) {
		s = s[1:]
	}
	if !strings.HasPrefix(s, "/") {
		return false
	}
	end := strings.IndexAny(s, stops)
	if end >= 0 {
		s = s[:end]
	}
	// Only a climb above the folder itself escapes it: "sub/../file" stays in.
	depth := 0
	for _, part := range strings.Split(s, "/") {
		switch part {
		case "", ".":
		case "..":
			depth--
			if depth < 0 {
				return true
			}
		default:
			depth++
		}
	}
	return false
}

// isNameByte reports bytes that can continue a folder name, so own ID
// "abc" doesn't let "abcd" through.
func isNameByte(b byte) bool {
	return b == '-' || b == '_' || b == '.' ||
		b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}

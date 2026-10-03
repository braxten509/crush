// Package secretguard stops agent tool calls from reading the folders where
// secure entry saves secrets, such as ~/.config/secrets and
// ~/.config/crush/secrets. Helper programs read those files themselves, so
// agents never need to. It guards against accidental or prompted reads; it
// is not a sandbox, since programs running as the user can still open them.
package secretguard

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
)

// Reason is what the agent sees when a tool call is blocked.
const Reason = "Blocked: this would read a locked secrets folder. Use a helper program that reads the secret itself; never read secrets through agent tools."

var (
	// A path into a secrets folder, or the folder itself.
	folderPath = regexp.MustCompile(`(?i)secrets/|/secrets([^a-z_-]|$)`)
	// The bare folder name next to a .config path, as in
	// `cd ~/.config && cat secrets/key`.
	secretsWord = regexp.MustCompile(`(?i)(^|[^a-z])secrets([^a-z_-]|$)`)
	configDir   = regexp.MustCompile(`(^|[\s~/"'=])\.config([^\w.-]|$)`)
	// Globs that could expand to a secrets folder, like ~/.config/sec*.
	configGlob = regexp.MustCompile(`(?i)\.config/(crush/)?s[^/\s"]*[*?\[]`)
	// Secure entry only writes a typed secret into a prepared file.
	secureEntry = regexp.MustCompile(`^\s*(\S*/)?crush\s+(secure-entry|ask)(\s|$)`)
	chained     = regexp.MustCompile("[;|&`]|\\$\\(")
)

// writesOnly reports tools that write or edit files without showing their
// contents, which never reveals a secret.
func writesOnly(tool string) bool {
	switch strings.ToLower(tool) {
	case "edit", "write", "multiedit", "notebookedit", "apply_patch":
		return true
	}
	return false
}

// Blocks reports whether a tool call with this name and JSON input would
// read a secrets folder.
func Blocks(tool string, input []byte) bool {
	if writesOnly(tool) {
		return false
	}
	var value any
	if json.Unmarshal(input, &value) != nil {
		value = string(input)
	}
	if fields, ok := value.(map[string]any); ok {
		if command, ok := fields["command"].(string); ok && secureEntry.MatchString(command) && !chained.MatchString(command) {
			return false
		}
	}
	text := strings.Join(stringsIn(value, nil), "\n")
	return folderPath.MatchString(text) ||
		(secretsWord.MatchString(text) && configDir.MatchString(text)) ||
		configGlob.MatchString(text)
}

// stringsIn collects every string in a decoded JSON value.
func stringsIn(value any, out []string) []string {
	switch v := value.(type) {
	case string:
		out = append(out, v)
	case []any:
		for _, item := range v {
			out = stringsIn(item, out)
		}
	case map[string]any:
		for _, item := range v {
			out = stringsIn(item, out)
		}
	}
	return out
}

// RunHook reads a PreToolUse hook event (the JSON Crush, Claude Code and
// Codex send on stdin) and returns the hook's exit code: 2 blocks the call,
// with the reason on stderr.
func RunHook(stdin io.Reader, stderr io.Writer) int {
	var event struct {
		ToolName  string          `json:"tool_name"`
		ToolInput json.RawMessage `json:"tool_input"`
	}
	data, err := io.ReadAll(stdin)
	if err != nil {
		return 0
	}
	if json.Unmarshal(data, &event) != nil || event.ToolInput == nil {
		// Unknown shapes are checked whole rather than let through.
		event.ToolInput = data
	}
	if Blocks(event.ToolName, event.ToolInput) {
		fmt.Fprintln(stderr, Reason)
		return 2
	}
	return 0
}

// HookCommand is the shell command CLI agents run as their PreToolUse hook.
func HookCommand() string {
	exe, err := os.Executable()
	if err != nil {
		exe = "crush"
	}
	return "'" + strings.ReplaceAll(exe, "'", `'\''`) + "' secrets-guard"
}

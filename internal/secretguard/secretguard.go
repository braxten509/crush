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
	"path/filepath"
	"regexp"
	"strings"

	"mvdan.cc/sh/v3/syntax"
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
)

// secureEntryOnly reports whether command is one `crush secure-entry` or
// `crush ask` call and nothing else, optionally fed by a here-doc. Secure
// entry only writes a typed secret into a prepared file, so it may name a
// secrets file. A second command, an input redirect or a command
// substitution could read one, so any of those (or text that does not
// parse) loses the exemption.
func secureEntryOnly(command string) bool {
	file, err := syntax.NewParser().Parse(strings.NewReader(command), "")
	if err != nil || len(file.Stmts) != 1 {
		return false
	}
	stmt := file.Stmts[0]
	if stmt.Background || stmt.Coprocess || stmt.Disown || stmt.Negated {
		return false
	}
	call, ok := stmt.Cmd.(*syntax.CallExpr)
	if !ok || len(call.Args) < 2 || filepath.Base(call.Args[0].Lit()) != "crush" {
		return false
	}
	if sub := call.Args[1].Lit(); sub != "secure-entry" && sub != "ask" {
		return false
	}
	for _, redirect := range stmt.Redirs {
		switch redirect.Op {
		case syntax.Hdoc, syntax.DashHdoc, syntax.DplOut:
		default:
			return false
		}
	}
	only := true
	syntax.Walk(file, func(node syntax.Node) bool {
		switch node.(type) {
		case *syntax.CmdSubst, *syntax.ProcSubst:
			only = false
		}
		return only
	})
	return only
}

// writesOnly reports tools that write or edit files without showing their
// contents, which never reveals a secret.
func writesOnly(tool string) bool {
	switch strings.ToLower(tool) {
	case "edit", "write", "multiedit", "notebookedit", "apply_patch",
		"edit_file", "write_file", "replace_file_content",
		"multi_replace_file_content", "write_to_file":
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
		if command, ok := fields["command"].(string); ok && secureEntryOnly(command) {
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

// BlockReason returns the refusal reason when a tool call trips any guard,
// or "" when allowed.
func BlockReason(tool string, input []byte, ownChatFolder string) string {
	return BlockReasonInDir(tool, input, ownChatFolder, "")
}

// BlockReasonInDir is like [BlockReason], resolving relative law-file paths
// against dir.
func BlockReasonInDir(tool string, input []byte, ownChatFolder, dir string) string {
	switch {
	case Blocks(tool, input):
		return Reason
	case BlocksWalletRead(tool, input):
		return WalletReason
	case BlocksChatFolder(tool, input, ownChatFolder):
		return ChatFolderReason
	case BlocksLawsInDir(tool, input, dir):
		return LawsReason
	case BlocksAgentCLI(input):
		return AgentCLIReason
	default:
		return ""
	}
}

// RunHook reads a PreToolUse hook event (the JSON Crush, Claude Code and
// Codex send on stdin) and returns the hook's exit code: 2 blocks the call,
// with the reason on stderr.
func RunHook(stdin io.Reader, stderr io.Writer) int {
	return RunHookIO(stdin, io.Discard, stderr)
}

// RunHookIO handles PreToolUse hook events from Crush, Claude Code, Codex,
// and Antigravity. Antigravity sends a camelCase toolCall object and reads a
// JSON decision from stdout; the other CLIs read stderr and exit code 2.
func RunHookIO(stdin io.Reader, stdout, stderr io.Writer) int {
	var event struct {
		CWD       string          `json:"cwd"`
		ToolName  string          `json:"tool_name"`
		ToolInput json.RawMessage `json:"tool_input"`
		ToolCall  *struct {
			Name string          `json:"name"`
			Args json.RawMessage `json:"args"`
		} `json:"toolCall"`
	}
	data, err := io.ReadAll(stdin)
	if err != nil {
		return 0
	}
	own := os.Getenv(ChatFolderEnv)
	if json.Unmarshal(data, &event) == nil && event.ToolCall != nil {
		reason := BlockReasonInDir(event.ToolCall.Name, event.ToolCall.Args, own, event.CWD)
		reply := map[string]string{"decision": "allow"}
		if reason != "" {
			reply["decision"] = "deny"
			reply["reason"] = reason
		}
		_ = json.NewEncoder(stdout).Encode(reply)
		return 0
	}
	if event.ToolInput == nil {
		// Unknown shapes are checked whole rather than let through.
		event.ToolInput = data
	}
	if reason := BlockReasonInDir(event.ToolName, event.ToolInput, own, event.CWD); reason != "" {
		fmt.Fprintln(stderr, reason)
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

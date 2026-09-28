package cliagent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/permission"
)

// claudeTool maps a Claude Code tool call onto the matching Crush tool so the
// UI renders it natively. Unknown tools keep their name and input.
func claudeTool(name string, raw json.RawMessage) (string, string) {
	var in map[string]any
	_ = json.Unmarshal(raw, &in)
	switch name {
	case "Bash":
		return tools.BashToolName, string(raw)
	case "Read":
		// Claude's offset is a 1-based line number; Crush's is 0-based.
		if off, ok := in["offset"].(float64); ok && off > 0 {
			in["offset"] = off - 1
		}
		return tools.ViewToolName, marshal(in)
	case "Edit":
		return tools.EditToolName, string(raw)
	case "Write":
		return tools.WriteToolName, string(raw)
	case "Grep":
		if g, ok := in["glob"]; ok {
			in["include"] = g
		}
		return tools.GrepToolName, marshal(in)
	case "Glob":
		return tools.GlobToolName, string(raw)
	case "WebFetch":
		return tools.WebFetchToolName, string(raw)
	case "WebSearch":
		return tools.WebSearchToolName, string(raw)
	}
	if rest, ok := strings.CutPrefix(name, "mcp__"); ok {
		return "mcp_" + strings.Replace(rest, "__", "_", 1), string(raw)
	}
	return name, string(raw)
}

// resultMetadata builds the metadata Crush's renderers read for a finished
// tool call (bash output, file content, edit diff).
func resultMetadata(name, input, output string) string {
	var in map[string]any
	_ = json.Unmarshal([]byte(input), &in)
	str := func(k string) string { s, _ := in[k].(string); return s }
	switch name {
	case tools.BashToolName:
		return marshal(tools.BashResponseMetadata{Output: output, Description: str("description")})
	case tools.ViewToolName:
		return marshal(tools.ViewResponseMetadata{FilePath: str("file_path"), Content: stripLineNumbers(output)})
	case tools.EditToolName:
		return marshal(tools.EditResponseMetadata{OldContent: str("old_string"), NewContent: str("new_string")})
	}
	return ""
}

var lineNumber = regexp.MustCompile(`^\s*\d+[\t→]`)

// stripLineNumbers removes the `cat -n` style prefixes Claude's Read tool
// adds, since Crush's viewer numbers lines itself. Output that doesn't
// look numbered is returned unchanged.
func stripLineNumbers(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, line := range lines {
		loc := lineNumber.FindStringIndex(line)
		if loc == nil {
			return s
		}
		lines[i] = line[loc[1]:]
	}
	return strings.Join(lines, "\n")
}

// autoApproved reports whether Crush would grant every tool call in the
// session anyway, so the CLI can skip asking.
func (m *Model) autoApproved(sessionID string) bool {
	if m.Guarded {
		return false
	}
	a, ok := m.Perms.(interface{ AutoApproved(string) bool })
	return ok && sessionID != "" && a.AutoApproved(sessionID)
}

// approve asks the user, through Crush's permission dialog, whether a CLI may
// run a tool. The request mirrors what Crush's own tool would ask, so
// "allow for session" and YOLO mode behave the same.
func (m *Model) approve(ctx context.Context, sessionID, callID, name, input string) bool {
	var in map[string]any
	_ = json.Unmarshal([]byte(input), &in)
	str := func(k string) string { s, _ := in[k].(string); return s }
	if sleepRefused(name, input) {
		slog.Info("Refused a foreground sleep", "command", str("command"))
		return false
	}
	if m.Guarded {
		if name == tools.BashToolName && tools.CommandBlocked(str("command")) {
			slog.Info("Blocked a sub-agent command", "command", str("command"))
			return false
		}
		return true
	}
	if m.Perms == nil || sessionID == "" {
		return false
	}
	if name == tools.BashToolName && IsSpawnCommand(str("command")) {
		// Starting a sub-agent is Crush's own feature; asking every time
		// defeats the point of running them in the background.
		return true
	}

	req := permission.CreatePermissionRequest{
		SessionID:  sessionID,
		ToolCallID: callID,
		ToolName:   name,
		Action:     "execute",
		Path:       m.Dir,
		Params:     in,
	}
	switch name {
	case tools.BashToolName:
		req.Description = "Execute command: " + str("command")
		req.Params = tools.BashPermissionsParams{Command: str("command"), Description: str("description"), WorkingDir: m.Dir}
	case tools.EditToolName:
		req.Action, req.Path = "write", str("file_path")
		req.Description = "Edit file " + req.Path
		req.Params = tools.EditPermissionsParams{FilePath: req.Path, OldContent: str("old_string"), NewContent: str("new_string")}
	case tools.WriteToolName:
		req.Action, req.Path = "write", str("file_path")
		req.Description = "Write file " + req.Path
		old, _ := os.ReadFile(req.Path)
		req.Params = tools.WritePermissionsParams{FilePath: req.Path, OldContent: string(old), NewContent: str("content")}
	case tools.ViewToolName:
		req.Action, req.Path = "read", str("file_path")
		req.Description = "Read file " + req.Path
		req.Params = tools.ViewPermissionsParams{FilePath: req.Path}
	default:
		req.Description = fmt.Sprintf("Run %s", name)
	}
	ok, err := m.Perms.Request(ctx, req)
	return err == nil && ok
}

// sleepRefused reports whether a tool call is a shell command that starts
// with a long foreground sleep, which Crush refuses as its own shell does.
func sleepRefused(name, input string) bool {
	var in struct{ Command string }
	return name == tools.BashToolName && json.Unmarshal([]byte(input), &in) == nil && tools.LeadingSleep(in.Command)
}

// IsSpawnCommand reports whether a shell command only runs `crush spawn` or
// `crush ask`:
// one or more lines of it, each optionally fed by a quoted heredoc, with
// nothing that could run anything else.
func IsSpawnCommand(cmd string) bool {
	lines := strings.Split(strings.TrimSpace(cmd), "\n")
	for len(lines) > 0 {
		line := strings.TrimSpace(lines[0])
		lines = lines[1:]
		if line == "" {
			continue
		}
		delim, ok := spawnLine(line)
		if !ok {
			return false
		}
		if delim == "" {
			continue
		}
		end := slices.Index(lines, delim)
		if end < 0 {
			return false
		}
		lines = lines[end+1:]
	}
	return true
}

// spawnLine checks one `crush spawn` or `crush ask` line and returns its
// heredoc delimiter, if it has one.
func spawnLine(line string) (delim string, ok bool) {
	rest, ok := "", false
	bin, _ := os.Executable()
	for _, prefix := range []string{"crush spawn", "crush ask", bin + " spawn", bin + " ask"} {
		if rest, ok = strings.CutPrefix(line, prefix); ok {
			break
		}
	}
	if !ok || (rest != "" && rest[0] != ' ') {
		return "", false
	}
	if i := strings.Index(rest, "<<"); i >= 0 {
		d := strings.TrimSpace(rest[i+2:])
		// Only a quoted delimiter keeps the shell from expanding the body.
		if len(d) < 3 || (d[0] != '\'' && d[0] != '"') || d[len(d)-1] != d[0] {
			return "", false
		}
		delim, rest = d[1:len(d)-1], rest[:i]
		if strings.ContainsAny(delim, "'\" \t") {
			return "", false
		}
	}
	var quote byte
	for i := 0; i < len(rest); i++ {
		c := rest[i]
		switch {
		case quote == '\'':
			if c == '\'' {
				quote = 0
			}
		case quote == '"':
			if c == '"' {
				quote = 0
			} else if c == '$' || c == '`' || c == '\\' {
				return "", false
			}
		case c == '\'' || c == '"':
			quote = c
		case strings.IndexByte(";&|`$<>(){}\\*?[#~", c) >= 0:
			return "", false
		}
	}
	return delim, quote == 0
}

// EditedFile returns the file a Crush edit or write call changes, if any.
func EditedFile(name, input string) string {
	if name != tools.EditToolName && name != tools.WriteToolName {
		return ""
	}
	var in struct {
		FilePath string `json:"file_path"`
	}
	_ = json.Unmarshal([]byte(input), &in)
	return in.FilePath
}

// RecordEdit adds a CLI's change to a file to the session's file history,
// as Crush's own edit tools do, so it shows under modified files.
func (m *Model) RecordEdit(ctx context.Context, sessionID, path, before string) {
	if m.Files == nil {
		return
	}
	after, err := os.ReadFile(path)
	if err != nil {
		return
	}
	if file, err := m.Files.GetByPathAndSession(ctx, path, sessionID); err != nil {
		_, err = m.Files.Create(ctx, sessionID, path, before)
		if err != nil {
			slog.Error("Error creating file history", "error", err)
			return
		}
	} else if file.Content != before {
		// Changed outside the session since; keep that version too.
		if _, err := m.Files.CreateVersion(ctx, sessionID, path, before); err != nil {
			slog.Error("Error creating file history version", "error", err)
		}
	}
	if _, err := m.Files.CreateVersion(ctx, sessionID, path, string(after)); err != nil {
		slog.Error("Error creating file history version", "error", err)
	}
}

func marshal(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(data)
}

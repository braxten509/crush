package cliagent

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"mvdan.cc/sh/v3/shell"

	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/diff"
	"github.com/charmbracelet/crush/internal/filechange"
	"github.com/charmbracelet/crush/internal/message"
)

// codexInput includes the captured image bytes, including clipboard images
// that have no persistent file on disk.
func codexInput(prompt string, attachments []message.Attachment) []any {
	var input []any
	if prompt != "" || !slices.ContainsFunc(attachments, message.Attachment.IsImage) {
		input = append(input, map[string]any{"type": "text", "text": prompt, "text_elements": []any{}})
	}
	for _, attachment := range attachments {
		if !attachment.IsImage() {
			continue
		}
		input = append(input, map[string]any{
			"type": "image",
			"url":  "data:" + attachment.MimeType + ";base64," + base64.StdEncoding.EncodeToString(attachment.Content),
		})
	}
	return input
}

// codexRequest answers a server request. Command and file approvals go to
// Crush's permission dialog; anything else is declined.
func codexRequest(ctx context.Context, m *Model, t Turn, p *proc, msg rpcMessage, params codexParams, changes []codexChange) {
	decide := func(ok bool) {
		decision := "decline"
		if ok {
			decision = "accept"
		}
		_ = p.send(map[string]any{"id": msg.ID, "result": map[string]any{"decision": decision}})
	}
	switch msg.Method {
	case "item/commandExecution/requestApproval":
		input := marshal(map[string]string{"command": unwrapShell(params.Command), "description": ""})
		decide(!t.NoTools && m.approve(ctx, t.SessionID, params.ItemID, tools.BashToolName, input))
	case "item/fileChange/requestApproval":
		ok := !t.NoTools
		for i, c := range changes {
			name, input := codexChangeTool(c)
			ok = ok && m.approve(ctx, t.SessionID, params.ItemID+"#"+strconv.Itoa(i), name, input)
		}
		decide(ok)
	default:
		_ = p.send(map[string]any{"id": msg.ID, "error": map[string]any{"code": -32601, "message": "not supported by Crush"}})
	}
}

// codexChangeTool presents one file of a Codex patch as a Crush write (new
// file) or edit (changed or deleted file).
func codexChangeTool(c codexChange) (string, string) {
	if c.Kind.Type == "add" {
		return tools.WriteToolName, marshal(tools.WriteParams{FilePath: c.Path, Content: c.Diff})
	}
	oldText, newText := splitDiff(c.Diff)
	if c.Kind.Type == "delete" {
		return tools.EditToolName, marshal(struct {
			tools.EditParams
			DeleteFile bool `json:"delete_file"`
		}{tools.EditParams{FilePath: c.Path, OldString: oldText, NewString: newText}, true})
	}
	return tools.EditToolName, marshal(tools.EditParams{FilePath: c.Path, OldString: oldText, NewString: newText})
}

// codexChangeMetadata describes a finished change with the whole file before
// and after it. The after text is read from disk; the before text comes from
// undoing the reported patch, so it does not depend on when Crush looked at
// the file. It returns "" when the patch no longer matches the file.
func codexChangeMetadata(dir string, c codexChange) string {
	if c.Kind.Type == "delete" {
		return filechange.WithReview("", &filechange.Review{Root: dir, Deletions: true})
	}
	if !filechange.InRepository(filechange.RepositoryRoot(dir), dir, c.Path) {
		return filechange.WithReview("", &filechange.Review{Root: dir})
	}
	path := c.Path
	if !filepath.IsAbs(path) {
		path = filepath.Join(dir, path)
	}
	var before, after string
	switch c.Kind.Type {
	case "add":
		data, err := os.ReadFile(path)
		if err != nil {
			return ""
		}
		after = string(data)
	default:
		data, err := os.ReadFile(path)
		if err != nil {
			return ""
		}
		after = string(data)
		var ok bool
		if before, ok = reversePatch(after, c.Diff); !ok {
			return ""
		}
	}
	_, adds, dels := diff.GenerateDiff(before, after, path)
	metadata := marshal(tools.EditResponseMetadata{Additions: adds, Removals: dels, OldContent: before, NewContent: after})
	mode := uint32(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = uint32(info.Mode())
	}
	change := filechange.Change{Path: path, Order: time.Now().UnixNano()}
	if c.Kind.Type != "add" {
		change.Before = &filechange.State{Content: before, Size: int64(len(before)), Mode: mode}
	}
	change.After = &filechange.State{Content: after, Size: int64(len(after)), Mode: mode}
	return filechange.WithReview(metadata, &filechange.Review{Root: dir, Changes: []filechange.Change{change}})
}

// reversePatch undoes a unified diff's hunks on the patched text, in order.
func reversePatch(patched, patch string) (string, bool) {
	type hunk struct {
		oldLines, newLines     []string
		at, oldCount, newCount int
	}
	header := regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)
	var hunks []hunk
	var current *hunk
	previous := byte(0)
	for _, line := range strings.Split(strings.TrimSuffix(patch, "\n"), "\n") {
		if strings.HasPrefix(line, "@@") {
			match := header.FindStringSubmatch(line)
			if match == nil {
				return "", false
			}
			at, _ := strconv.Atoi(match[3])
			oldCount, newCount := 1, 1
			if match[2] != "" {
				oldCount, _ = strconv.Atoi(match[2])
			}
			if match[4] != "" {
				newCount, _ = strconv.Atoi(match[4])
			}
			if newCount > 0 {
				at--
			}
			hunks = append(hunks, hunk{at: at, oldCount: oldCount, newCount: newCount})
			current = &hunks[len(hunks)-1]
			previous = 0
			continue
		}
		if current == nil {
			continue
		}
		if strings.HasPrefix(line, `\ No newline at end of file`) {
			if (previous == '-' || previous == ' ') && len(current.oldLines) > 0 {
				n := len(current.oldLines) - 1
				current.oldLines[n] = strings.TrimSuffix(current.oldLines[n], "\n")
			}
			if (previous == '+' || previous == ' ') && len(current.newLines) > 0 {
				n := len(current.newLines) - 1
				current.newLines[n] = strings.TrimSuffix(current.newLines[n], "\n")
			}
			continue
		}
		if line == "" {
			return "", false
		}
		previous = line[0]
		switch previous {
		case '-':
			current.oldLines = append(current.oldLines, line[1:]+"\n")
		case '+':
			current.newLines = append(current.newLines, line[1:]+"\n")
		case ' ':
			current.oldLines = append(current.oldLines, line[1:]+"\n")
			current.newLines = append(current.newLines, line[1:]+"\n")
		default:
			return "", false
		}
	}
	if len(hunks) == 0 {
		return "", false
	}
	lines := strings.SplitAfter(patched, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	var output strings.Builder
	position := 0
	for _, h := range hunks {
		if len(h.oldLines) != h.oldCount || len(h.newLines) != h.newCount || h.at < position || h.at+len(h.newLines) > len(lines) {
			return "", false
		}
		if !slices.Equal(lines[h.at:h.at+len(h.newLines)], h.newLines) {
			return "", false
		}
		output.WriteString(strings.Join(lines[position:h.at], ""))
		output.WriteString(strings.Join(h.oldLines, ""))
		position = h.at + len(h.newLines)
	}
	output.WriteString(strings.Join(lines[position:], ""))
	return output.String(), true
}

// splitDiff rebuilds the before and after text of a unified diff's hunks,
// which is what Crush's edit view diffs.
func splitDiff(diff string) (string, string) {
	var before, after strings.Builder
	for i, line := range strings.Split(strings.TrimRight(diff, "\n"), "\n") {
		switch {
		case strings.HasPrefix(line, "@@"):
			if i > 0 {
				before.WriteString("…\n")
				after.WriteString("…\n")
			}
		case strings.HasPrefix(line, "-"):
			before.WriteString(line[1:] + "\n")
		case strings.HasPrefix(line, "+"):
			after.WriteString(line[1:] + "\n")
		case strings.HasPrefix(line, `\`): // "\ No newline at end of file"
		default:
			line = strings.TrimPrefix(line, " ")
			before.WriteString(line + "\n")
			after.WriteString(line + "\n")
		}
	}
	return before.String(), after.String()
}

var shellWrapper = regexp.MustCompile(`(?s)^\S*sh -l?c (?:'(.*)'|"(.*)")$`)

// unwrapShell turns Codex's `/bin/zsh -lc 'cmd'` into `cmd` for display.
func unwrapShell(cmd string) string {
	// Providers may wrap an already wrapped command. Decode each shell argv
	// rather than stripping its first and last quote: mixed shell quoting can
	// start with a single quote and end with a double quote.
	for {
		decoded := unwrapShellOnce(cmd)
		if decoded == cmd {
			return cmd
		}
		cmd = decoded
	}
}

func unwrapShellOnce(cmd string) string {
	m := shellWrapper.FindStringSubmatch(cmd)
	if m != nil && m[2] != "" {
		// Preserve Codex's JSON-style command representation, including newlines.
		if command, err := strconv.Unquote(`"` + m[2] + `"`); err == nil {
			return command
		}
	}
	words, err := shell.Fields(cmd, func(name string) string { return "$" + name })
	if err == nil && len(words) == 3 && strings.HasSuffix(filepath.Base(words[0]), "sh") &&
		(words[1] == "-lc" || words[1] == "-c") {
		return words[2]
	}
	return cmd
}

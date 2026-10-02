package chat

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/ui/diffreview"
)

// Changes returns the file changes the group's finished tool calls made,
// one diff per file. Cached until a step or result changes.
func (g *ToolGroupItem) Changes() []diffreview.File {
	var key strings.Builder
	for _, c := range g.children {
		fmt.Fprintf(&key, "%s:%d;", c.ID(), c.Version())
	}
	if k := key.String(); k == g.changesKey {
		return g.changes
	}
	g.changesKey = key.String()
	var edits []diffreview.Edit
	for _, c := range g.children {
		t, ok := c.(ToolMessageItem)
		if !ok {
			continue
		}
		r, ok := t.(interface{ Result() *message.ToolResult })
		if !ok || r.Result() == nil {
			continue
		}
		if review := r.Result().Review; review != nil {
			for _, change := range review.Changes {
				if ignoredReviewPath(change.Path, review.Root) {
					continue
				}
				edits = append(edits, diffreview.Edit{Path: change.Path, Snapshot: &change})
			}
			continue
		}
		if r.Result().IsError {
			continue
		}
		if e, ok := toolEdit(t.ToolCall(), r.Result()); ok && !ignoredReviewPath(e.Path, "") {
			edits = append(edits, e)
		}
	}
	g.changes = diffreview.Build(edits)
	return g.changes
}

// toolEdit reads the change a file tool made from its result metadata.
func toolEdit(tc message.ToolCall, r *message.ToolResult) (diffreview.Edit, bool) {
	switch tc.Name {
	case tools.EditToolName, tools.MultiEditToolName, tools.WriteToolName:
	default:
		return diffreview.Edit{}, false
	}
	var in tools.EditParams
	_ = json.Unmarshal([]byte(tc.Input), &in)
	e := diffreview.Edit{Path: in.FilePath}
	if e.Path == "" || r.Metadata == "" {
		return e, false
	}
	// Edits (and CLI writes, which Crush diffs itself) carry the file's
	// content before and after; Crush's own write carries a unified diff.
	var meta struct {
		tools.EditResponseMetadata
		Diff string `json:"diff"`
	}
	if json.Unmarshal([]byte(r.Metadata), &meta) != nil {
		return e, false
	}
	switch {
	case meta.OldContent != "" || meta.NewContent != "":
		e.Before, e.After = meta.OldContent, meta.NewContent
		// Older CLI edits stored only the replaced text, which has no
		// place in the file, and no line counts.
		e.Full = tc.Name != tools.EditToolName || meta.Additions+meta.Removals > 0 ||
			in.OldString != e.Before || in.NewString != e.After
	case meta.Diff != "":
		e.Unified = meta.Diff
	default:
		return e, false
	}
	return e, true
}

// ChangesRequester is an item whose click asked to review its changes.
type ChangesRequester interface {
	TakeChangesRequest() bool
}

// TakeChangesRequest reports whether the last click was on the group's
// changes, and clears it.
func (g *ToolGroupItem) TakeChangesRequest() bool {
	r := g.changesRequested
	g.changesRequested = false
	return r
}

// ChangesTitle describes the group for the changes overlay.
func (g *ToolGroupItem) ChangesTitle() string {
	n := 0
	for _, c := range g.children {
		if _, ok := c.(ToolMessageItem); ok {
			n++
		}
	}
	return actionCount(n)
}

// actionCount says "1 action" or "N actions".
func actionCount(n int) string {
	if n == 1 {
		return "1 action"
	}
	return fmt.Sprintf("%d actions", n)
}

// Filter at presentation time so old saved tool results follow the same rules
// as new ones. Never read or enumerate the filesystem to decide visibility.
func ignoredReviewPath(path, root string) bool {
	if root != "" && !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	path = filepath.Clean(path)
	home, _ := os.UserHomeDir()
	if hiddenFolderPath(path, home) || agentToolPath(path) {
		return true
	}
	cache, _ := os.UserCacheDir()
	configuration, _ := os.UserConfigDir()
	// Devices and kernel files (/dev/tty, /sys/fs/cgroup/...) are written by
	// the programs a command starts; they are never edits.
	excluded := []string{os.TempDir(), cache, configuration, "/tmp", "/var/tmp", "/dev", "/proc", "/sys"}
	// Application data and state are noise too, including shared agent memory
	// and sessions.
	for _, location := range []struct{ variable, fallback string }{
		{"XDG_DATA_HOME", "share"},
		{"XDG_STATE_HOME", "state"},
	} {
		directory := os.Getenv(location.variable)
		if !filepath.IsAbs(directory) && home != "" {
			directory = filepath.Join(home, ".local", location.fallback)
		}
		if filepath.IsAbs(directory) {
			excluded = append(excluded, directory)
		}
	}
	for _, directory := range excluded {
		if directory == "" {
			continue
		}
		directory = filepath.Clean(directory)
		if path == directory || strings.HasPrefix(path, directory+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// Instruction files the user reads and edits stay visible even inside an
// agent's folder.
var instructionFiles = map[string]bool{
	"AGENTS.md": true, "CLAUDE.md": true, "CLAUDE.local.md": true, "CRUSH.md": true, "GEMINI.md": true,
}

// hiddenFolderPath reports a path inside any folder whose name starts with a
// dot (.git, .claude-flow, .github, ...). Dot files themselves (.gitignore,
// .env) stay visible, and so does ~/.local/bin, which holds user-written tools.
func hiddenFolderPath(path, home string) bool {
	if instructionFiles[filepath.Base(path)] {
		return false
	}
	if home != "" {
		tools := filepath.Join(home, ".local", "bin")
		if path == tools || strings.HasPrefix(path, tools+string(filepath.Separator)) {
			return false
		}
	}
	folders := strings.Split(filepath.Dir(path), string(filepath.Separator))
	for _, folder := range folders {
		if strings.HasPrefix(folder, ".") && folder != "." && folder != ".." {
			return true
		}
	}
	return false
}

// agentToolPath reports state that agent tooling keeps outside dot folders:
// agent databases, skill lockfiles and swarm runtimes.
func agentToolPath(path string) bool {
	if instructionFiles[filepath.Base(path)] {
		return false
	}
	for _, component := range strings.Split(path, string(filepath.Separator)) {
		name := strings.ToLower(component)
		if name == "skills-lock.json" || strings.HasPrefix(name, "agentdb.") ||
			strings.HasPrefix(name, "claude-flow") || strings.HasPrefix(name, "ruflo") {
			return true
		}
	}
	return false
}

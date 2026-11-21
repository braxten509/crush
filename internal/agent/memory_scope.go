package agent

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/charmbracelet/crush/internal/fsext"
	"github.com/charmbracelet/crush/internal/gitutil"
)

var memoryLink = regexp.MustCompile(`^-\s+\[.*?\]\(([^()]+\.md)\)`)

type memoryScope struct {
	workingDir string
	mainDir    string
}

func newMemoryScope(workingDir string) memoryScope {
	workingDir = memoryPath(workingDir)
	return memoryScope{workingDir: workingDir, mainDir: gitutil.MainWorktreePath(workingDir)}
}

// filter selects the project sections from an index or note and keeps the
// general text. In the index, it puts project entries before General so a
// long index can't crowd them out and records all links before truncation.
func (scope memoryScope) filter(index string) (string, map[string]bool) {
	var preamble, general, project strings.Builder
	section := &preamble
	allowed := make(map[string]bool)
	for _, line := range strings.SplitAfter(index, "\n") {
		text := strings.TrimSpace(line)
		if path, found := strings.CutPrefix(text, "## Project:"); found {
			section = nil
			root := memoryPath(strings.TrimSpace(path))
			if root != "" && (fsext.HasPrefix(scope.workingDir, root) || fsext.HasPrefix(scope.mainDir, root)) {
				section = &project
			}
		} else if text == "## General" {
			section = &general
		}
		if match := memoryLink.FindStringSubmatch(text); match != nil {
			name := filepath.Base(match[1])
			allowed[name] = allowed[name] || section != nil
		}
		if section != nil {
			section.WriteString(line)
		}
	}
	if project.Len() == 0 {
		return preamble.String() + general.String(), allowed
	}
	return strings.TrimRight(preamble.String(), "\r\n") + "\n\n" +
		strings.TrimRight(project.String(), "\r\n") + "\n\n" + general.String(), allowed
}

func memoryPath(path string) string {
	if rest, found := strings.CutPrefix(path, "~/"); found || path == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		path = filepath.Join(home, rest)
	}
	if !filepath.IsAbs(path) {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return filepath.Clean(path)
}

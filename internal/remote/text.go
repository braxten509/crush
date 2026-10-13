package remote

import (
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/crush/internal/fsext"
)

// clip keeps the tail of long output: the end of a command is what matters.
func clip(s string) (string, bool) {
	s = strings.TrimRight(s, "\n")
	cut := false
	if lines := strings.Split(s, "\n"); len(lines) > detailMaxLines {
		s, cut = strings.Join(lines[len(lines)-detailMaxLines:], "\n"), true
	}
	if len(s) > detailMaxBytes {
		s, cut = s[len(s)-detailMaxBytes:], true
		for len(s) > 0 && !utf8.RuneStart(s[0]) {
			s = s[1:]
		}
	}
	return s, cut
}

// small helpers

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i]) + " …"
	}
	return s
}

func shortPath(p, wd string) string {
	if p == "" {
		return ""
	}
	if wd != "" {
		if rel, err := filepath.Rel(wd, p); err == nil && !strings.HasPrefix(rel, "..") {
			return rel
		}
	}
	return fsext.PrettyPath(p)
}

func firstStringParam(in map[string]any) string {
	for _, k := range []string{"path", "file_path", "query", "url", "name", "symbol", "command"} {
		if s, ok := in[k].(string); ok && s != "" {
			return firstLine(s)
		}
	}
	for _, v := range in {
		if s, ok := v.(string); ok && s != "" {
			return firstLine(s)
		}
	}
	return ""
}

func countLines(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return strconv.Itoa(n) + " " + word + "s"
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func between(s, a, b string) string {
	_, rest, ok := strings.Cut(s, a)
	if !ok {
		return ""
	}
	v, _, _ := strings.Cut(rest, b)
	return strings.TrimSpace(v)
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

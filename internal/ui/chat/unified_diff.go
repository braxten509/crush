package chat

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/crush/internal/diff"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/styles"
)

type parsedDiffFile struct {
	path   string
	before string
	after  string
}

func looksLikeDiff(content string) bool {
	return diff.IsUnifiedDiff(content)
}

func parseUnifiedDiff(content string) []parsedDiffFile {
	p := unifiedDiffParser{current: -1}
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		nextIsPlusHeader := i+1 < len(lines) && strings.HasPrefix(lines[i+1], "+++ ")
		p.line(line, nextIsPlusHeader)
	}

	result := make([]parsedDiffFile, 0, len(p.files))
	for _, f := range p.files {
		result = append(result, parsedDiffFile{
			path:   f.path,
			before: strings.TrimSuffix(f.before.String(), "\n"),
			after:  strings.TrimSuffix(f.after.String(), "\n"),
		})
	}
	return result
}

// unifiedDiffParser collects the files of a unified diff, one line at a
// time, rebuilding each file's before and after text from its hunks.
type unifiedDiffParser struct {
	files []diffFileBuilder
	// current indexes the file lines belong to; -1 before the first.
	current int
	inHunk  bool
}

type diffFileBuilder struct {
	path   string
	before strings.Builder
	after  strings.Builder
}

// line consumes one line. nextIsPlusHeader says whether the following
// line starts a "+++ " header, which makes a "--- " line a header even
// inside a hunk.
func (p *unifiedDiffParser) line(line string, nextIsPlusHeader bool) {
	switch {
	case strings.HasPrefix(line, "diff --git "):
		p.inHunk = false
		parts := strings.SplitN(line, " ", 4)
		if len(parts) >= 4 {
			p.startFile(strings.TrimPrefix(parts[3], "b/"))
		}
	case strings.HasPrefix(line, "@@"):
		p.inHunk = true
	case strings.HasPrefix(line, "index ") || strings.HasPrefix(line, "new file") || strings.HasPrefix(line, "deleted file"):
		p.inHunk = false
	case strings.HasPrefix(line, "--- ") && (!p.inHunk || nextIsPlusHeader):
		p.oldFileHeader(line, p.inHunk && nextIsPlusHeader)
	case strings.HasPrefix(line, "+++ ") && !p.inHunk:
		p.newFileHeader(line)
	case p.current >= 0:
		p.hunkLine(line)
	}
}

// startFile begins a new file at path.
func (p *unifiedDiffParser) startFile(path string) {
	p.files = append(p.files, diffFileBuilder{path: path})
	p.current = len(p.files) - 1
}

// oldFileHeader handles a "--- " header. One that directly follows a hunk
// starts a new file, as does the first header of a diff without "diff --git"
// lines.
func (p *unifiedDiffParser) oldFileHeader(line string, startedNewFileFromHunk bool) {
	p.inHunk = false
	path := diffHeaderPath(line, "--- ", "a/")
	if p.current < 0 || startedNewFileFromHunk {
		p.startFile(path)
		return
	}
	if path != "/dev/null" {
		p.files[p.current].path = path
	}
}

// newFileHeader handles a "+++ " header, which names the file when the
// "--- " header did not (a new file, or no header at all).
func (p *unifiedDiffParser) newFileHeader(line string) {
	path := diffHeaderPath(line, "+++ ", "b/")
	if path == "/dev/null" {
		return
	}
	if p.current < 0 {
		p.startFile(path)
		return
	}
	if cur := &p.files[p.current]; cur.path == "" || strings.HasPrefix(cur.path, "/dev/null") {
		cur.path = path
	}
}

// hunkLine adds a removed, added or context line to the current file.
func (p *unifiedDiffParser) hunkLine(line string) {
	cur := &p.files[p.current]
	switch {
	case strings.HasPrefix(line, "-"):
		p.inHunk = true
		cur.before.WriteString(line[1:])
		cur.before.WriteByte('\n')
	case strings.HasPrefix(line, "+"):
		p.inHunk = true
		cur.after.WriteString(line[1:])
		cur.after.WriteByte('\n')
	case strings.HasPrefix(line, " "):
		p.inHunk = true
		lineContent := line[1:]
		cur.before.WriteString(lineContent)
		cur.before.WriteByte('\n')
		cur.after.WriteString(lineContent)
		cur.after.WriteByte('\n')
	}
}

// diffHeaderPath returns the path of a "--- " or "+++ " header line,
// without its "a/" or "b/" side prefix and any tab-separated timestamp.
func diffHeaderPath(line, header, side string) string {
	path := strings.TrimPrefix(line, header)
	path = strings.TrimPrefix(path, side)
	if idx := strings.Index(path, "\t"); idx >= 0 {
		path = path[:idx]
	}
	return path
}

func toolOutputDiffContentFromUnified(sty *styles.Styles, content string, width int, expanded bool) string {
	files := parseUnifiedDiff(content)
	if len(files) == 0 {
		bodyWidth := width - toolBodyLeftPaddingTotal
		return sty.Tool.Body.Render(toolOutputCodeContent(sty, "result.diff", content, 0, bodyWidth, expanded))
	}
	bodyWidth := width - toolBodyLeftPaddingTotal
	var blocks []string
	for i, f := range files {
		formatter := common.DiffFormatter(sty).
			Before(f.path, f.before).
			After(f.path, f.after).
			Width(bodyWidth)
		if len(files) > 1 {
			formatter = formatter.FileName(f.path)
		}
		if width > maxTextWidth {
			formatter = formatter.Split()
		}
		formatted := formatter.String()
		if i < len(files)-1 {
			formatted += "\n"
		}
		blocks = append(blocks, formatted)
	}
	combined := strings.Join(blocks, "\n")
	lines := strings.Split(combined, "\n")
	maxLines := collapsedMaxLines(len(lines))
	if expanded {
		maxLines = len(lines)
	}
	if len(lines) > maxLines && !expanded {
		truncMsg := sty.Tool.DiffTruncation.
			Width(bodyWidth).
			Render(fmt.Sprintf(assistantMessageTruncateFormat, len(lines)-maxLines))
		combined = strings.Join(lines[:maxLines], "\n") + "\n" + truncMsg
	}
	return sty.Tool.Body.Render(combined)
}

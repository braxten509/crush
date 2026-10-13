// Package diffreview turns the file changes a run of tool calls made into
// per-file hunks with line numbers, for the changes overlay.
package diffreview

import (
	"cmp"
	"fmt"
	"io/fs"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/aymanbagabas/go-udiff"
	"github.com/charmbracelet/crush/internal/filechange"
)

// Kind is what happened to a file.
type Kind int

const (
	Modified Kind = iota
	Added
	Deleted
	Copied
	Moved
	Generated
)

func (k Kind) String() string {
	switch k {
	case Added:
		return "Added"
	case Deleted:
		return "Deleted"
	case Copied:
		return "Copied"
	case Moved:
		return "Moved"
	case Generated:
		return "Generated"
	}
	return "Changed"
}

// LineKind is the role of a diff line.
type LineKind int

const (
	Context LineKind = iota
	Add
	Del
	Hunk
)

// Line is one row of a file's diff. Old and New are 1-based line numbers,
// 0 where the side has no line or the number isn't known.
type Line struct {
	Kind     LineKind
	Old, New int
	Text     string
}

// File is the combined change to one file.
type File struct {
	Path       string
	Kind       Kind
	Lines      []Line
	Adds, Dels int
	Transfer   string
	// Omitted marks a file whose text wasn't compared (too large, or not
	// text), so it has no line counts.
	Omitted bool
}

// Counted reports whether the file's line counts mean anything: its text
// was compared and it still exists. A deleted file counts as one removed
// file, not as removed lines.
func (f File) Counted() bool {
	return !f.Omitted && f.Kind != Deleted
}

// AnyCounted reports whether any of files has line counts to show.
func AnyCounted(files []File) bool {
	return slices.ContainsFunc(files, File.Counted)
}

// Removed counts the deleted files.
func Removed(files []File) int {
	n := 0
	for _, f := range files {
		if f.Kind == Deleted {
			n++
		}
	}
	return n
}

// Edit is one tool call's change to a file. Full edits carry the whole file
// before and after; others carry only the replaced text (no line numbers)
// or a unified diff.
type Edit struct {
	Path          string
	Before, After string
	Full          bool
	Unified       string
	// Snapshot records presence and metadata as well as text. It takes
	// precedence over legacy edit-tool metadata.
	Snapshot *filechange.Change
}

// contextLines is how much unchanged text surrounds each hunk.
const contextLines = 3

// Build combines edits into one diff per file, in the order the files were
// first changed. A file whose edits all carry the whole file shows as one
// diff from before its first edit to after its last; otherwise each edit
// shows on its own.
func Build(edits []Edit) []File {
	var order []string
	byPath := map[string][]Edit{}
	for _, e := range edits {
		if _, ok := byPath[e.Path]; !ok {
			order = append(order, e.Path)
		}
		byPath[e.Path] = append(byPath[e.Path], e)
	}
	var files []File
	movedSources := map[string]bool{}
	for _, es := range byPath {
		captured := true
		for _, e := range es {
			captured = captured && e.Snapshot != nil
		}
		if !captured {
			continue
		}
		slices.SortStableFunc(es, func(a, b Edit) int { return cmp.Compare(a.Snapshot.Order, b.Snapshot.Order) })
		first, last := es[0].Snapshot, es[len(es)-1].Snapshot
		if first.Transfer != nil && first.Transfer.Kind == "move" && last.After != nil {
			movedSources[first.Transfer.Source] = true
		}
	}
	for _, path := range order {
		es := byPath[path]
		captured := true
		for _, e := range es {
			captured = captured && e.Snapshot != nil
		}
		if captured {
			change := *es[0].Snapshot
			if change.Transfer != nil {
				transfer := *change.Transfer
				transfer.Baseline = change.ImportedState()
				change.Transfer = &transfer
			}
			change.After = es[len(es)-1].Snapshot.After
			if movedSources[path] && change.After == nil {
				continue
			}
			if f, ok := fromSnapshot(change); ok {
				files = append(files, f)
			}
			continue
		}
		full := true
		for _, e := range es {
			full = full && e.Full && e.Snapshot == nil
		}
		if full {
			if f, ok := fromContent(path, es[0].Before, es[len(es)-1].After, true); ok {
				files = append(files, f)
			}
			continue
		}
		for _, e := range es {
			var f File
			var ok bool
			if e.Snapshot != nil {
				f, ok = fromSnapshot(*e.Snapshot)
			} else if e.Unified != "" {
				f, ok = fromUnified(path, e.Unified)
			} else {
				f, ok = fromContent(path, e.Before, e.After, e.Full)
			}
			if ok {
				files = append(files, f)
			}
		}
	}
	return files
}

func fromSnapshot(change filechange.Change) (File, bool) {
	if transfer := change.Transfer; transfer != nil && change.Before == nil && change.After != nil {
		// Imported lines are the baseline. Only later edits get +/- counts.
		f, _ := fromSnapshot(filechange.Change{Path: change.Path, Before: change.ImportedState(), After: change.After})
		f.Path, f.Kind, f.Transfer = change.Path, Copied, transfer.Kind
		label := "Copied file"
		if transfer.Kind == "checkout" {
			label = "Copied from repository checkout"
		}
		if transfer.Kind == "move" {
			f.Kind, label = Moved, "Moved from "+transfer.Source
		}
		if transfer.Kind == "generated" {
			f.Kind, label = Generated, "Generated build artifact"
		}
		f.Lines = append([]Line{{Text: label}}, f.Lines...)
		return f, true
	}
	before, after := change.Before, change.After
	if before == nil && after == nil || before != nil && after != nil && *before == *after {
		return File{}, false
	}
	// Terminals, pipes and sockets aren't files anyone edits; their only
	// "change" is a timestamp. Older saved reviews still contain them.
	if specialFile(before) || specialFile(after) {
		return File{}, false
	}
	f := File{Path: change.Path}
	switch {
	case before == nil:
		f.Kind = Added
	case after == nil:
		f.Kind = Deleted
	}
	note := func(text string) { f.Lines = append(f.Lines, Line{Text: text}) }
	if before != nil && after != nil && before.Mode != after.Mode {
		note(fmt.Sprintf("Mode: %s → %s", fs.FileMode(before.Mode), fs.FileMode(after.Mode)))
	}
	var oldText, newText string
	var oldSize, newSize int64
	omitted := ""
	if before != nil {
		oldText, oldSize, omitted = before.Content, before.Size, before.Omitted
	}
	if after != nil {
		newText, newSize = after.Content, after.Size
		if after.Omitted != "" {
			omitted = after.Omitted
		}
	}
	if omitted != "" {
		f.Omitted = true
		note(fmt.Sprintf("%s; %d → %d bytes", omitted, oldSize, newSize))
	} else if content, ok := fromContent(change.Path, oldText, newText, true); ok {
		f.Lines = append(f.Lines, content.Lines...)
		f.Adds, f.Dels = content.Adds, content.Dels
	} else if len(f.Lines) == 0 {
		note(f.Kind.String() + " empty file")
	}
	return f, true
}

func specialFile(state *filechange.State) bool {
	return state != nil && strings.HasPrefix(state.Omitted, "Special file")
}

// Stats sums the added and removed lines of the files that have counts.
func Stats(files []File) (adds, dels int) {
	for _, f := range files {
		if !f.Counted() {
			continue
		}
		adds += f.Adds
		dels += f.Dels
	}
	return adds, dels
}

func fromContent(path, before, after string, numbered bool) (File, bool) {
	if before == after {
		return File{}, false
	}
	f := File{Path: path}
	if numbered {
		switch {
		case before == "":
			f.Kind = Added
		case after == "":
			f.Kind = Deleted
		}
	}
	ud, err := udiff.ToUnifiedDiff(path, path, before, udiff.Lines(before, after), contextLines)
	if err != nil {
		return File{}, false
	}
	for _, h := range ud.Hunks {
		oldNo, newNo := h.FromLine, h.ToLine
		if numbered {
			f.Lines = append(f.Lines, Line{Kind: Hunk, Text: hunkHeader(h)})
		} else if len(f.Lines) > 0 {
			f.Lines = append(f.Lines, Line{Kind: Hunk, Text: "…"})
		}
		for _, l := range h.Lines {
			text := strings.TrimSuffix(l.Content, "\n")
			ln := Line{Text: text}
			switch l.Kind {
			case udiff.Insert:
				ln.Kind, ln.New = Add, newNo
				newNo++
				f.Adds++
			case udiff.Delete:
				ln.Kind, ln.Old = Del, oldNo
				oldNo++
				f.Dels++
			default:
				ln.Old, ln.New = oldNo, newNo
				oldNo++
				newNo++
			}
			if !numbered {
				ln.Old, ln.New = 0, 0
			}
			f.Lines = append(f.Lines, ln)
		}
	}
	return f, len(f.Lines) > 0
}

func hunkHeader(h *udiff.Hunk) string {
	var from, to int
	for _, l := range h.Lines {
		switch l.Kind {
		case udiff.Insert:
			to++
		case udiff.Delete:
			from++
		default:
			from++
			to++
		}
	}
	return "@@ -" + span(h.FromLine, from) + " +" + span(h.ToLine, to) + " @@"
}

func span(start, n int) string {
	if n == 0 {
		start--
	}
	if n == 1 {
		return strconv.Itoa(start)
	}
	return strconv.Itoa(start) + "," + strconv.Itoa(n)
}

var hunkRe = regexp.MustCompile(`^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@`)

// fromUnified reads the hunks of a single-file unified diff.
func fromUnified(path, diff string) (File, bool) {
	f := File{Path: path}
	var oldNo, newNo int
	inHunk := false
	for _, line := range strings.Split(diff, "\n") {
		if m := hunkRe.FindStringSubmatch(line); m != nil {
			oldNo, _ = strconv.Atoi(m[1])
			newNo, _ = strconv.Atoi(m[2])
			inHunk = true
			f.Lines = append(f.Lines, Line{Kind: Hunk, Text: line})
			continue
		}
		if !inHunk {
			if line == "--- /dev/null" {
				f.Kind = Added
			} else if line == "+++ /dev/null" {
				f.Kind = Deleted
			}
			continue
		}
		if line == "" {
			continue
		}
		switch line[0] {
		case '+':
			f.Lines = append(f.Lines, Line{Kind: Add, New: newNo, Text: line[1:]})
			newNo++
			f.Adds++
		case '-':
			f.Lines = append(f.Lines, Line{Kind: Del, Old: oldNo, Text: line[1:]})
			oldNo++
			f.Dels++
		case ' ':
			f.Lines = append(f.Lines, Line{Old: oldNo, New: newNo, Text: line[1:]})
			oldNo++
			newNo++
		}
	}
	return f, f.Adds+f.Dels > 0
}

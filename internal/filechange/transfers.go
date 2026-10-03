package filechange

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

type moveSource struct {
	path  string
	state *State
	info  fs.FileInfo
}

func (source moveSource) movedTo(destination string) bool {
	_, err := os.Lstat(source.path)
	if !os.IsNotExist(err) {
		return false
	}
	info, err := os.Lstat(destination)
	return err == nil && os.SameFile(source.info, info)
}

// moving records the two paths from an actual rename system call. A failed
// rename is left as an ordinary review because its source still exists.
func (p *ProcessReview) moving(record *invocation, source, destination string) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	if record == nil || record.tracker == nil {
		return
	}
	entry, ok := record.tracker.files[source]
	if !ok || !entry.info.Mode().IsRegular() {
		return
	}
	if record.moves == nil {
		record.moves = map[string]moveSource{}
	}
	state := entry.state
	record.moves[destination] = moveSource{path: source, state: &state, info: entry.info}
}

// Transfer identifies existing content brought into a new path. Baseline is
// only stored when it differs from After, avoiding a duplicate full snapshot.
type Transfer struct {
	Kind     string `json:"kind"`
	Source   string `json:"source,omitempty"`
	Baseline *State `json:"baseline,omitempty"`
}

// ImportedState returns the baseline for edits made after a transfer.
func (change Change) ImportedState() *State {
	if change.Transfer != nil && change.Transfer.Baseline != nil {
		return change.Transfer.Baseline
	}
	return change.After
}

// CommandTransferKind recognizes a single literal copy/clone in older saved
// tool results. Compound commands must retain their ordinary edit counts.
func CommandTransferKind(command string) string {
	file, err := syntax.NewParser().Parse(strings.NewReader(command), "")
	if err != nil || len(file.Stmts) != 1 {
		return ""
	}
	stmt := file.Stmts[0]
	call, ok := stmt.Cmd.(*syntax.CallExpr)
	if !ok || len(stmt.Redirs) != 0 || stmt.Background || len(call.Assigns) != 0 {
		return ""
	}
	var args []string
	for _, word := range call.Args {
		var arg strings.Builder
		for _, part := range word.Parts {
			switch part := part.(type) {
			case *syntax.Lit:
				arg.WriteString(part.Value)
			case *syntax.SglQuoted:
				arg.WriteString(part.Value)
			case *syntax.DblQuoted:
				for _, quoted := range part.Parts {
					lit, ok := quoted.(*syntax.Lit)
					if !ok {
						return ""
					}
					arg.WriteString(lit.Value)
				}
			default:
				return ""
			}
		}
		args = append(args, arg.String())
	}
	return transferKind(args)
}

// transferKind uses the executed process, never text embedded in a script.
// Unknown Git options fall back to an ordinary review rather than hiding edits.
func transferKind(args []string) string {
	if len(args) == 0 {
		return ""
	}
	switch filepath.Base(args[0]) {
	case "cp":
		return "copy"
	case "git":
		for i := 1; i < len(args); i++ {
			switch arg := args[i]; {
			case arg == "-C" || arg == "-c" || arg == "--git-dir" || arg == "--work-tree":
				i++
			case strings.HasPrefix(arg, "--git-dir=") || strings.HasPrefix(arg, "--work-tree=") || arg == "--no-pager" || arg == "--no-optional-locks":
			case arg == "clone":
				return "checkout"
			default:
				return ""
			}
		}
	}
	return ""
}

// mergeChanges preserves the earliest baseline in capture order. A later
// writer's before image also records where an import ended, even when the
// clone and edit were reported together in one shell call.
func mergeChanges(changes []Change) []Change {
	slices.SortStableFunc(changes, func(a, b Change) int {
		if a.Order < b.Order {
			return -1
		}
		if a.Order > b.Order {
			return 1
		}
		return 0
	})
	var merged []Change
	indices := map[string]int{}
	importFinished := map[string]bool{}
	for _, change := range changes {
		if index, ok := indices[change.Path]; ok {
			previous := &merged[index]
			if previous.Transfer != nil && !importFinished[change.Path] && change.Before != nil {
				transfer := *previous.Transfer
				transfer.Baseline = change.Before
				previous.Transfer = &transfer
				importFinished[change.Path] = true
			}
			previous.After = change.After
		} else {
			indices[change.Path] = len(merged)
			merged = append(merged, change)
		}
	}
	return merged
}

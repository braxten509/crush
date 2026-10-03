package diffreview

import (
	"fmt"
	"strings"
)

// TransferSummary describes imported files without calling their contents edits.
func TransferSummary(files []File) string {
	counts := map[string]int{}
	for _, file := range files {
		counts[file.Transfer]++
	}
	var parts []string
	for _, kind := range []string{"checkout", "copy", "move", "generated"} {
		if n := counts[kind]; n > 0 {
			label := map[string]string{"checkout": "copied checkout:", "copy": "copied", "move": "moved", "generated": "generated"}[kind]
			noun := "files"
			if n == 1 {
				noun = "file"
			}
			parts = append(parts, fmt.Sprintf("%s %d %s", label, n, noun))
		}
	}
	return strings.Join(parts, " · ")
}

// HasEdits distinguishes an imported file from an authored content change.
func (f File) HasEdits() bool {
	return f.Transfer == "" || f.Adds != 0 || f.Dels != 0 || len(f.Lines) > 1
}

// Summary keeps copied and moved files distinct from authored changes.
func Summary(files []File) string {
	transfer := TransferSummary(files)
	if transfer == "" {
		noun := "files"
		if len(files) == 1 {
			noun = "file"
		}
		return fmt.Sprintf("%d %s changed", len(files), noun)
	}
	edited := 0
	for _, file := range files {
		if file.HasEdits() {
			edited++
		}
	}
	if edited > 0 {
		noun := "files"
		if edited == 1 {
			noun = "file"
		}
		transfer += fmt.Sprintf(" · %d %s edited", edited, noun)
	}
	return transfer
}

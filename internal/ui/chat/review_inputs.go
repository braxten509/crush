package chat

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/message"
)

// ReviewInput is an immutable snapshot for an asynchronously opened drawer.
type ReviewInput struct {
	Call   message.ToolCall
	Result message.ToolResult
}

func (g *ToolGroupItem) ReviewInputs() []ReviewInput {
	var inputs []ReviewInput
	for _, child := range g.children {
		tool, ok := child.(ToolMessageItem)
		if !ok {
			continue
		}
		result, ok := tool.(interface{ Result() *message.ToolResult })
		if !ok || result.Result() == nil {
			continue
		}
		inputs = append(inputs, ReviewInput{Call: tool.ToolCall(), Result: *result.Result()})
	}
	return inputs
}

// StoredReviewSummary reads only small saved summaries, never file contents.
func (g *ToolGroupItem) StoredReviewSummary() string {
	_ = g.Changes()
	return g.storedSummary
}

func (g *ToolGroupItem) computeStoredReviewSummary() string {
	files, copied, checkouts, moved, generated, removed := 0, 0, 0, 0, 0, 0
	g.storedAdds, g.storedDels, g.storedCounted = 0, 0, false
	var names []string
	for _, input := range g.ReviewInputs() {
		review := input.Result.Review
		if review == nil || review.Summary == nil {
			continue
		}
		summary := review.Summary
		paths := summary.Paths
		if len(paths) == 0 {
			var params tools.EditParams
			_ = json.Unmarshal([]byte(input.Call.Input), &params)
			if params.FilePath != "" {
				if ignoredReviewPath(params.FilePath, "") {
					continue
				}
				paths = []string{params.FilePath}
			}
		}
		files += summary.Files
		if summary.Counted && !summary.NoLines {
			g.storedAdds += summary.Adds
			g.storedDels += summary.Dels
			g.storedCounted = true
		}
		copied += summary.Copied
		checkouts += summary.Checkouts
		moved += summary.Moved
		generated += summary.Generated
		removed += summary.Removed
		for _, path := range paths {
			// Each file once, at its latest edit.
			names = append(slices.DeleteFunc(names, func(n string) bool { return n == path }), path)
		}
	}
	var labels []string
	noun := func(n int) string {
		if n == 1 {
			return "file"
		}
		return "files"
	}
	for _, group := range []struct {
		label string
		count int
	}{{"copied checkout:", checkouts}, {"copied", copied}, {"moved", moved}, {"generated", generated}, {"removed", removed}} {
		if group.count > 0 {
			labels = append(labels, fmt.Sprintf("%s %d %s", group.label, group.count, noun(group.count)))
		}
	}
	if changed := files - copied - checkouts - moved - generated - removed; changed > 0 {
		if len(names) > 0 {
			// One name at most, the latest file; the rest are a count, as
			// in the live header.
			label := "edited " + filepath.Base(names[len(names)-1])
			if more := len(names) - 1; more > 0 {
				label += fmt.Sprintf(" and %d more", more)
			}
			labels = append(labels, label)
		} else {
			labels = append(labels, fmt.Sprintf("%d %s changed", changed, noun(changed)))
		}
	}
	return strings.Join(labels, " · ")
}

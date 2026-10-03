package chat

import (
	"encoding/json"
	"fmt"
	"path/filepath"
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
	files, copied, checkouts, moved, generated := 0, 0, 0, 0, 0
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
		copied += summary.Copied
		checkouts += summary.Checkouts
		moved += summary.Moved
		generated += summary.Generated
		for _, path := range paths {
			if len(names) < 3 {
				names = append(names, filepath.Base(path))
			}
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
	}{{"copied checkout:", checkouts}, {"copied", copied}, {"moved", moved}, {"generated", generated}} {
		if group.count > 0 {
			labels = append(labels, fmt.Sprintf("%s %d %s", group.label, group.count, noun(group.count)))
		}
	}
	if changed := files - copied - checkouts - moved - generated; changed > 0 {
		if len(names) > 0 && files <= 3 {
			labels = append(labels, "edited "+strings.Join(names, ", "))
		} else {
			labels = append(labels, fmt.Sprintf("%d %s changed", changed, noun(changed)))
		}
	}
	return strings.Join(labels, " · ")
}

package chat

import (
	"encoding/json"
	"path/filepath"
	"slices"

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
	g.storedDeletions = false
	g.storedAdds, g.storedDels, g.storedCounted = 0, 0, false
	var names []string
	complete := true
	for _, input := range g.ReviewInputs() {
		review := input.Result.Review
		if review == nil {
			continue
		}
		g.storedDeletions = g.storedDeletions || review.Deletions
		if review.Summary == nil {
			continue
		}
		summary := review.Summary
		g.storedDeletions = g.storedDeletions || summary.Deletions
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
		// Paths keeps at most a few files; past that only counts are known.
		complete = complete && summary.Files <= len(paths)+summary.Removed
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
	transfers := copied + checkouts + moved + generated
	edited := files - transfers - removed
	if complete && len(names) > 0 {
		// Each file once, however many edits saved it; never more than the
		// saved counts allow.
		edited = min(edited, len(names)-transfers)
	}
	name := ""
	if len(names) == 1 {
		name = filepath.Base(names[0])
	}
	return fileChangeLabel(map[string]int{
		"edited": max(edited, 0), "copied": copied, "checked out": checkouts,
		"moved": moved, "generated": generated, "removed": removed,
	}, name)
}

package chat

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/filechange"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/ui/diffreview"
)

// Changes returns the file changes the group's finished tool calls made,
// one diff per file. Cached until a step or result changes.
func (g *ToolGroupItem) Changes() []diffreview.File {
	var key strings.Builder
	for _, c := range g.children {
		result, ok := c.(interface{ Result() *message.ToolResult })
		if !ok || result.Result() == nil {
			continue
		}
		fmt.Fprintf(&key, "%s:%d;", c.ID(), c.Version())
	}
	if k := key.String(); k == g.changesKey {
		return g.changes
	}
	g.changesKey = key.String()
	g.changes = BuildReviewChanges(g.ReviewInputs())
	g.storedSummary = g.computeStoredReviewSummary()
	return g.changes
}

// BuildReviewChanges prepares a review off the UI thread.
func BuildReviewChanges(inputs []ReviewInput) []diffreview.File {
	var edits []diffreview.Edit
	var unavailable []diffreview.File
	for _, input := range inputs {
		if review := input.Result.Review; review != nil {
			if review.Summary != nil {
				continue
			}
			var commandInput struct {
				Command string `json:"command"`
			}
			if input.Call.Name == tools.BashToolName {
				_ = json.Unmarshal([]byte(input.Call.Input), &commandInput)
			}
			kind := filechange.CommandTransferKind(commandInput.Command)
			for _, change := range review.Changes {
				if ignoredReviewPath(change.Path, review.Root) {
					continue
				}
				if change.Transfer == nil && kind != "" && change.Before == nil && change.After != nil {
					change.Transfer = &filechange.Transfer{Kind: kind}
				}
				edits = append(edits, diffreview.Edit{Path: change.Path, Snapshot: &change})
			}
			continue
		}
		if input.Result.IsError {
			continue
		}
		var metadata struct {
			Omitted string `json:"review_omitted"`
		}
		_ = json.Unmarshal([]byte(input.Result.Metadata), &metadata)
		if metadata.Omitted != "" {
			var params tools.EditParams
			_ = json.Unmarshal([]byte(input.Call.Input), &params)
			if params.FilePath != "" && !ignoredReviewPath(params.FilePath, "") {
				unavailable = append(unavailable, diffreview.File{Path: params.FilePath, Lines: []diffreview.Line{{Text: metadata.Omitted}}})
			}
			continue
		}
		if e, ok := toolEdit(input.Call, &input.Result); ok && !ignoredReviewPath(e.Path, "") {
			edits = append(edits, e)
		}
	}
	return append(diffreview.Build(edits), unavailable...)
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

func ignoredReviewPath(path, root string) bool { return filechange.HiddenReviewPath(path, root) }

package remote

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/diff"
	"github.com/charmbracelet/crush/internal/message"
)

// DiffLine is one line of a file change as the phone draws it.
type DiffLine struct {
	Kind string `json:"k"` // "+", "-", " ", "@"
	Old  int    `json:"o,omitempty"`
	New  int    `json:"n,omitempty"`
	Text string `json:"t"`
}

// editDiff returns the lines an edit, multi-edit or write changes.
func editDiff(tc message.ToolCall, r message.ToolResult) []DiffLine {
	switch tc.Name {
	case tools.EditToolName:
		var meta tools.EditResponseMetadata
		if json.Unmarshal([]byte(r.Metadata), &meta) == nil && (meta.OldContent != "" || meta.NewContent != "") {
			return unifiedLines(meta.OldContent, meta.NewContent)
		}
		var p tools.EditParams
		if json.Unmarshal([]byte(tc.Input), &p) == nil {
			return unifiedLines(p.OldString, p.NewString)
		}
	case tools.MultiEditToolName:
		var meta tools.MultiEditResponseMetadata
		if json.Unmarshal([]byte(r.Metadata), &meta) == nil && (meta.OldContent != "" || meta.NewContent != "") {
			return unifiedLines(meta.OldContent, meta.NewContent)
		}
		var p tools.MultiEditParams
		if json.Unmarshal([]byte(tc.Input), &p) == nil {
			var out []DiffLine
			for _, e := range p.Edits {
				out = append(out, unifiedLines(e.OldString, e.NewString)...)
			}
			return out
		}
	case tools.WriteToolName:
		var meta tools.WriteResponseMetadata
		if json.Unmarshal([]byte(r.Metadata), &meta) == nil && meta.Diff != "" {
			return parseUnified(meta.Diff)
		}
		var p tools.WriteParams
		if json.Unmarshal([]byte(tc.Input), &p) == nil && p.Content != "" {
			if looksUnified(p.Content) {
				return parseUnified(p.Content)
			}
			return unifiedLines("", p.Content)
		}
	}
	return nil
}

func unifiedLines(before, after string) []DiffLine {
	if before == after {
		return nil
	}
	text, _, _ := diff.GenerateDiff(before, after, "file")
	return parseUnified(text)
}

func looksUnified(s string) bool {
	return strings.HasPrefix(s, "--- ") || strings.HasPrefix(s, "@@ ") || strings.HasPrefix(s, "diff ")
}

// parseUnified reads a unified diff into numbered lines.
func parseUnified(text string) []DiffLine {
	var out []DiffLine
	oldNo, newNo := 0, 0
	for line := range strings.SplitSeq(strings.TrimRight(text, "\n"), "\n") {
		switch {
		case strings.HasPrefix(line, "--- "), strings.HasPrefix(line, "+++ "), strings.HasPrefix(line, "diff "), strings.HasPrefix(line, "index "):
			continue
		case strings.HasPrefix(line, "@@"):
			var a, b int
			fmt.Sscanf(strings.TrimPrefix(line, "@@ "), "-%d", &a)
			if i := strings.Index(line, "+"); i >= 0 {
				fmt.Sscanf(line[i:], "+%d", &b)
			}
			oldNo, newNo = a, b
			out = append(out, DiffLine{Kind: "@", Text: line})
		case strings.HasPrefix(line, "+"):
			out = append(out, DiffLine{Kind: "+", New: newNo, Text: line[1:]})
			newNo++
		case strings.HasPrefix(line, "-"):
			out = append(out, DiffLine{Kind: "-", Old: oldNo, Text: line[1:]})
			oldNo++
		case strings.HasPrefix(line, `\`):
			continue
		default:
			out = append(out, DiffLine{Kind: " ", Old: oldNo, New: newNo, Text: strings.TrimPrefix(line, " ")})
			oldNo++
			newNo++
		}
	}
	return out
}

// StepDetail is what the phone loads when it opens a step.
type StepDetail struct {
	ID     string     `json:"id"`
	Input  string     `json:"input,omitempty"`
	Diff   []DiffLine `json:"diff,omitempty"`
	Output string     `json:"output,omitempty"`
	// Truncated says output or diff was cut to keep the phone fast.
	Truncated bool `json:"truncated,omitempty"`
	// Image is a media id when the result is an image.
	Image string `json:"image,omitempty"`
}

const (
	detailMaxLines = 400
	detailMaxBytes = 64 << 10
)

// stepDetail builds the detail of one step from the session's messages.
func stepDetail(msgs []message.Message, id string) (StepDetail, bool) {
	var call *message.ToolCall
	var res *message.ToolResult
	var resMsg string
	for _, m := range msgs {
		for _, tc := range m.ToolCalls() {
			if tc.ID == id {
				c := tc
				call = &c
			}
		}
		for i, r := range m.ToolResults() {
			if r.ToolCallID == id {
				rr := r
				res = &rr
				resMsg = m.ID + "/r" + strconv.Itoa(i)
			}
		}
	}
	if call == nil {
		return StepDetail{}, false
	}
	d := StepDetail{ID: id, Input: stepInput(*call)}
	var r message.ToolResult
	if res != nil {
		r = *res
	}
	if lines := editDiff(*call, r); len(lines) > 0 {
		if len(lines) > detailMaxLines {
			lines, d.Truncated = lines[:detailMaxLines], true
		}
		d.Diff = lines
	}
	if res == nil {
		return d, true
	}
	if res.Data != "" && strings.HasPrefix(res.MIMEType, "image/") {
		d.Image = resMsg
	}
	if len(d.Diff) == 0 {
		d.Output, d.Truncated = clip(stepOutput(*call, *res))
	}
	return d, true
}

func stepInput(tc message.ToolCall) string {
	var in map[string]any
	if json.Unmarshal([]byte(tc.Input), &in) != nil {
		return tc.Input
	}
	if cmd, ok := in["command"].(string); ok {
		return cmd
	}
	b, _ := json.MarshalIndent(in, "", "  ")
	return string(b)
}

func stepOutput(tc message.ToolCall, r message.ToolResult) string {
	switch tc.Name {
	case tools.BashToolName:
		var meta tools.BashResponseMetadata
		if json.Unmarshal([]byte(r.Metadata), &meta) == nil && meta.Output != "" {
			return meta.Output
		}
		if r.Content == tools.BashNoOutput {
			return ""
		}
	case tools.ViewToolName:
		var meta tools.ViewResponseMetadata
		if json.Unmarshal([]byte(r.Metadata), &meta) == nil && meta.Content != "" {
			return meta.Content
		}
	}
	return r.Content
}

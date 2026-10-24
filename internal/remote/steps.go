package remote

import (
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/charmbracelet/crush/internal/agent"
	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/message"
)

// groupSummary mirrors the TUI group header: the files a group edited and
// how many steps failed.
func groupSummary(steps []Step) string {
	var edited []string
	seen := map[string]bool{}
	failed := 0
	actions := 0
	for _, s := range steps {
		if s.Kind == "think" {
			continue
		}
		actions++
		if s.State == "error" {
			failed++
		}
		if (s.Kind == "edit" || s.Kind == "write") && s.Target != "" && !seen[s.Target] {
			seen[s.Target] = true
			edited = append(edited, filepath.Base(s.Target))
		}
	}
	parts := []string{plural(actions, "action")}
	if len(edited) > 0 {
		files := strings.Join(edited[:min(3, len(edited))], ", ")
		if len(edited) > 3 {
			files += fmt.Sprintf(" +%d", len(edited)-3)
		}
		parts = append(parts, "edited "+files)
	}
	if failed > 0 {
		parts = append(parts, fmt.Sprintf("%d failed", failed))
	}
	return strings.Join(parts, " · ")
}

// groupActivity says what a live group is doing now, as the TUI's status.
func groupActivity(steps []Step) string {
	if len(steps) == 0 {
		return "Thinking"
	}
	last := steps[len(steps)-1]
	switch last.State {
	case "waiting":
		return "Waiting for approval"
	case "running":
		if last.Kind == "think" {
			return "Thinking"
		}
		return activityFor(last.Kind)
	}
	return "Thinking"
}

func activityFor(kind string) string {
	switch kind {
	case "bash":
		return "Waiting for a tool response..."
	case "read":
		return "Reading a file"
	case "edit", "write":
		return "Editing a file"
	case "search":
		return "Searching"
	case "web":
		return "Searching the web"
	case "fetch":
		return "Reading a web page"
	case "todo":
		return "Updating the plan"
	case "agent":
		return "Running a sub-agent"
	}
	return "Working"
}

func toolStep(tc message.ToolCall, res *resultAt, m message.Message, st chatState) Step {
	in := map[string]any{}
	_ = json.Unmarshal([]byte(tc.Input), &in)
	str := func(k string) string { s, _ := in[k].(string); return s }
	s := Step{ID: tc.ID, Started: m.CreatedAt}
	s.Kind, s.Tool = toolKind(tc.Name)

	switch tc.Name {
	case tools.BashToolName:
		s.Target = firstLine(str("command"))
	case tools.JobOutputToolName, tools.JobKillToolName:
		s.Target = str("shell_id")
	case tools.ViewToolName, tools.EditToolName, tools.MultiEditToolName, tools.WriteToolName:
		s.Target = shortPath(str("file_path"), st.workingDir)
	case tools.GrepToolName:
		s.Target = str("pattern")
		if p := str("path"); p != "" {
			s.Target += " in " + shortPath(p, st.workingDir)
		}
	case tools.GlobToolName:
		s.Target = str("pattern")
	case tools.LSToolName:
		s.Target = shortPath(cmpOr(str("path"), "."), st.workingDir)
	case tools.FetchToolName, tools.WebFetchToolName, tools.DownloadToolName, tools.AgenticFetchToolName:
		s.Target = str("url")
	case tools.WebSearchToolName, tools.SourcegraphToolName:
		s.Target = str("query")
	case agent.AgentToolName:
		s.Target = firstLine(str("prompt"))
	case tools.TodosToolName:
		if todos, ok := in["todos"].([]any); ok {
			s.Target = plural(len(todos), "item")
		}
	default:
		s.Target = firstStringParam(in)
	}

	switch {
	case res != nil:
		s.Ended = res.at
		switch {
		case res.IsError:
			s.State = "error"
		case backgrounded(tc.Name, res.ToolResult):
			s.State = "background"
		default:
			s.State = "done"
		}
		s.Added, s.Removed, s.Meta = st.figuresFor(tc, res.ToolResult, "r"+strconv.Itoa(len(res.Content)+len(res.Metadata)))
	case tc.ID == st.waitingCall:
		s.State = "waiting"
	case m.IsFinished() && m.FinishReason() != message.FinishReasonToolUse, !st.busy:
		s.State = "stopped"
	default:
		s.State = "running"
	}
	if res == nil && (tc.Name == tools.EditToolName || tc.Name == tools.MultiEditToolName || tc.Name == tools.WriteToolName) {
		// Show the size of a pending edit before it lands.
		s.Added, s.Removed, _ = st.figuresFor(tc, message.ToolResult{}, "i"+strconv.Itoa(len(tc.Input)))
	}
	return s
}

func toolKind(name string) (kind, label string) {
	switch name {
	case tools.BashToolName:
		return "bash", "BASH"
	case tools.JobOutputToolName:
		return "bash", "OUTPUT"
	case tools.JobKillToolName:
		return "bash", "KILL"
	case tools.ViewToolName:
		return "read", "READ"
	case tools.EditToolName, tools.MultiEditToolName:
		return "edit", "EDIT"
	case tools.WriteToolName:
		return "write", "WRITE"
	case tools.GrepToolName:
		return "search", "GREP"
	case tools.GlobToolName:
		return "search", "GLOB"
	case tools.LSToolName:
		return "search", "LIST"
	case tools.SourcegraphToolName:
		return "search", "CODE"
	case tools.FetchToolName, tools.WebFetchToolName, tools.AgenticFetchToolName:
		return "fetch", "FETCH"
	case tools.DownloadToolName:
		return "fetch", "GET"
	case tools.WebSearchToolName:
		return "web", "WEB"
	case tools.TodosToolName:
		return "todo", "PLAN"
	case tools.DiagnosticsToolName:
		return "other", "CHECK"
	case agent.AgentToolName:
		return "agent", "AGENT"
	}
	if strings.HasPrefix(name, "lsp_") {
		return "other", "LSP"
	}
	if rest, ok := strings.CutPrefix(name, "mcp_"); ok {
		server, _, _ := strings.Cut(rest, "_")
		return "other", strings.ToUpper(truncate(server, 6))
	}
	return "other", strings.ToUpper(truncate(name, 6))
}

func backgrounded(name string, r message.ToolResult) bool {
	if name != tools.BashToolName {
		return false
	}
	var meta tools.BashResponseMetadata
	if json.Unmarshal([]byte(r.Metadata), &meta) == nil && meta.Background {
		return true
	}
	return strings.HasPrefix(r.Content, "Moved to the background")
}

// stepFigures returns the added and removed line counts of an edit, or a
// short fact about any other step ("42 lines", "3 matches").
func stepFigures(tc message.ToolCall, r message.ToolResult) (added, removed int, meta string) {
	switch tc.Name {
	case tools.EditToolName, tools.MultiEditToolName, tools.WriteToolName:
		// The edit tools report their counts; diff only when they don't.
		var meta struct {
			Additions int `json:"additions"`
			Removals  int `json:"removals"`
		}
		if json.Unmarshal([]byte(r.Metadata), &meta) == nil && meta.Additions+meta.Removals > 0 {
			return meta.Additions, meta.Removals, ""
		}
		for _, l := range editDiff(tc, r) {
			switch l.Kind {
			case "+":
				added++
			case "-":
				removed++
			}
		}
		return added, removed, ""
	case tools.ViewToolName:
		var meta tools.ViewResponseMetadata
		if json.Unmarshal([]byte(r.Metadata), &meta) == nil && meta.Content != "" {
			return 0, 0, plural(strings.Count(meta.Content, "\n")+1, "line")
		}
	case tools.GrepToolName, tools.GlobToolName, tools.LSToolName:
		if n := countLines(r.Content); n > 0 && !r.IsError {
			return 0, 0, plural(n, "line")
		}
	case tools.FetchToolName, tools.WebFetchToolName, tools.DownloadToolName:
		var in struct {
			URL string `json:"url"`
		}
		_ = json.Unmarshal([]byte(tc.Input), &in)
		if u, err := url.Parse(in.URL); err == nil && u.Host != "" {
			return 0, 0, u.Host
		}
	}
	return 0, 0, ""
}

package remote

import (
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/crush/internal/agent"
	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/diff"
	"github.com/charmbracelet/crush/internal/fsext"
	"github.com/charmbracelet/crush/internal/message"
)

// The phone draws the chat from Items rather than raw messages: tool calls
// are already paired with their results and folded into status groups the
// way the TUI folds them (internal/ui/chat/toolgroup.go), so the app only
// renders. Heavy parts (diffs, command output) stay on the PC until the
// phone opens a step; see stepDetail.

// Item is one entry of the phone's chat list.
type Item struct {
	ID   string `json:"id"`
	Kind string `json:"kind"` // user, reply, thinking, group, ask, notice, error
	// Time is when the entry was created, in unix seconds.
	Time int64 `json:"time,omitempty"`

	Text      string `json:"text,omitempty"`
	Streaming bool   `json:"streaming,omitempty"`
	// Seconds is how long a thinking block took.
	Seconds int `json:"seconds,omitempty"`

	Attachments []Attachment `json:"attachments,omitempty"`

	Steps []Step `json:"steps,omitempty"`
	// Live marks the group at the end of the chat while the agent works.
	Live bool `json:"live,omitempty"`
	// Activity says what a live group is doing ("Running a command").
	Activity string `json:"activity,omitempty"`
	// Summary names the files a finished group edited and its failures.
	Summary string `json:"summary,omitempty"`

	// Answer is the reply to an ask record; Cancelled means none came.
	Answer    string `json:"answer,omitempty"`
	Cancelled bool   `json:"cancelled,omitempty"`

	// Tone colors a notice: plain, good, stop.
	Tone string `json:"tone,omitempty"`
}

// Attachment is a file the user sent with a message.
type Attachment struct {
	ID    string `json:"id"` // media id: <message id>/<index>
	Name  string `json:"name"`
	Mime  string `json:"mime"`
	Image bool   `json:"image,omitempty"`
}

// Step is one tool call (or thinking-only step) inside a group.
type Step struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"` // read, search, edit, write, bash, fetch, web, todo, agent, think, other
	Tool    string `json:"tool"` // short label: READ, EDIT, BASH...
	Target  string `json:"target,omitempty"`
	State   string `json:"state"` // running, waiting, done, error, stopped, background
	Added   int    `json:"added,omitempty"`
	Removed int    `json:"removed,omitempty"`
	Meta    string `json:"meta,omitempty"`
	Started int64  `json:"started,omitempty"`
	Ended   int64  `json:"ended,omitempty"`
}

// chatState is what the item builder needs besides the messages.
type chatState struct {
	busy bool
	// waitingCall is the tool call waiting for a permission answer.
	waitingCall string
	workingDir  string
	// figures caches step figures by tool call id, since diffing an edit
	// again on every update is costly. Nil disables the cache.
	figures map[string]figures
}

type figures struct {
	key            string
	added, removed int
	meta           string
}

// figuresFor is stepFigures through the cache. key changes whenever the
// inputs can have changed.
func (st chatState) figuresFor(tc message.ToolCall, r message.ToolResult, key string) (int, int, string) {
	if f, ok := st.figures[tc.ID]; ok && f.key == key {
		return f.added, f.removed, f.meta
	}
	added, removed, meta := stepFigures(tc, r)
	if st.figures != nil {
		st.figures[tc.ID] = figures{key: key, added: added, removed: removed, meta: meta}
	}
	return added, removed, meta
}

// entry is an item before tool steps are folded into groups.
type entry struct {
	item     Item
	step     *Step
	foldable bool
	thinking bool // a thinking-only assistant step
}

// buildItems turns a session's messages into the phone's chat list.
func buildItems(msgs []message.Message, st chatState) []Item {
	results := map[string]resultAt{}
	for _, m := range msgs {
		if m.Role != message.Tool {
			continue
		}
		for _, r := range m.ToolResults() {
			results[r.ToolCallID] = resultAt{r, m.UpdatedAt}
		}
	}

	var entries []entry
	for i, m := range msgs {
		last := i == len(msgs)-1
		switch m.Role {
		case message.User:
			if e, ok := userEntry(m); ok {
				entries = append(entries, e)
			}
		case message.Assistant:
			entries = append(entries, assistantEntries(m, last, results, st)...)
		}
	}
	return fold(entries, st)
}

type resultAt struct {
	message.ToolResult
	at int64
}

func userEntry(m message.Message) (entry, bool) {
	c := m.Content()
	if c.Hidden {
		return entry{}, false
	}
	text := strings.TrimSpace(c.Text)
	if name, status, ok := agent.ParseTaskNotification(text); ok {
		return entry{item: Item{ID: m.ID, Kind: "notice", Time: m.CreatedAt, Text: taskNotice(name, status), Tone: taskTone(name, status)}}, true
	}
	if strings.HasPrefix(text, "<loaded_skill>") {
		name := between(text, "<name>", "</name>")
		return entry{item: Item{ID: m.ID, Kind: "notice", Time: m.CreatedAt, Text: "Skill " + name + " loaded", Tone: "plain"}}, true
	}
	it := Item{ID: m.ID, Kind: "user", Time: m.CreatedAt, Text: text}
	for i, b := range m.BinaryContent() {
		name := filepath.Base(b.Path)
		if b.Path == "" {
			name = "attachment " + strconv.Itoa(i+1)
		}
		it.Attachments = append(it.Attachments, Attachment{
			ID:    m.ID + "/" + strconv.Itoa(i),
			Name:  name,
			Mime:  b.MIMEType,
			Image: strings.HasPrefix(b.MIMEType, "image/"),
		})
	}
	if it.Text == "" && len(it.Attachments) == 0 {
		return entry{}, false
	}
	return entry{item: it}, true
}

func taskNotice(name, status string) string {
	if name == agent.AskName {
		if status == agent.AskCancelled {
			return "Questions closed without answers"
		}
		return "You answered the questions"
	}
	switch agent.TaskStatus(status) {
	case agent.TaskFailed:
		return "Task " + name + " failed"
	case agent.TaskStopped:
		return "Task " + name + " stopped"
	}
	return "Task " + name + " finished"
}

func taskTone(name, status string) string {
	switch {
	case name == agent.AskName && status == agent.AskCancelled:
		return "stop"
	case status == string(agent.TaskFailed), status == string(agent.TaskStopped):
		return "stop"
	}
	return "good"
}

func assistantEntries(m message.Message, last bool, results map[string]resultAt, st chatState) []entry {
	var out []entry
	finished := m.IsFinished()
	calls := m.ToolCalls()
	text := strings.TrimSpace(m.Content().Text)

	if r := m.ReasoningContent(); strings.TrimSpace(r.Thinking) != "" {
		it := Item{
			ID:        m.ID + ":think",
			Kind:      "thinking",
			Time:      m.CreatedAt,
			Text:      strings.TrimSpace(r.Thinking),
			Seconds:   int(m.ThinkingDuration().Seconds()),
			Streaming: r.FinishedAt == 0 && !finished && st.busy && last,
		}
		onlyThinking := text == "" && len(calls) == 0
		e := entry{item: it, thinking: onlyThinking, foldable: onlyThinking}
		if onlyThinking {
			e.step = &Step{ID: it.ID, Kind: "think", Tool: "THINK", Target: firstLine(it.Text), State: stepStateThinking(it.Streaming), Started: m.CreatedAt}
		}
		out = append(out, e)
	}
	if text != "" {
		out = append(out, entry{item: Item{
			ID:        m.ID,
			Kind:      "reply",
			Time:      m.CreatedAt,
			Text:      text,
			Streaming: !finished && st.busy && last,
		}})
	}
	for _, tc := range calls {
		r, hasResult := results[tc.ID]
		var res *resultAt
		if hasResult {
			res = &r
		}
		switch tc.Name {
		case tools.QuestionToolName:
			out = append(out, entry{item: askItem(tc, res, m.CreatedAt)})
			continue
		}
		s := toolStep(tc, res, m, st)
		e := entry{step: &s, foldable: true}
		switch tc.Name {
		case agent.AgentToolName, tools.AgenticFetchToolName:
			// Not folded with the steps around it, as in the TUI.
			e.foldable = false
		}
		out = append(out, e)
	}
	switch m.FinishReason() {
	case message.FinishReasonCanceled:
		out = append(out, entry{item: Item{ID: m.ID + ":finish", Kind: "notice", Time: m.CreatedAt, Text: "Stopped", Tone: "stop"}})
	case message.FinishReasonError:
		f := m.FinishPart()
		msg := strings.TrimSpace(f.Message)
		if f.Details != "" {
			msg = strings.TrimSpace(msg + "\n" + f.Details)
		}
		if msg == "" {
			msg = "The agent hit an error."
		}
		out = append(out, entry{item: Item{ID: m.ID + ":finish", Kind: "error", Time: m.CreatedAt, Text: msg}})
	case message.FinishReasonContentFilter:
		out = append(out, entry{item: Item{ID: m.ID + ":finish", Kind: "notice", Time: m.CreatedAt, Text: "The model refused to answer", Tone: "stop"}})
	case message.FinishReasonMaxTokens:
		out = append(out, entry{item: Item{ID: m.ID + ":finish", Kind: "notice", Time: m.CreatedAt, Text: "The reply hit the output limit", Tone: "plain"}})
	}
	return out
}

func stepStateThinking(streaming bool) string {
	if streaming {
		return "running"
	}
	return "done"
}

func askItem(tc message.ToolCall, res *resultAt, at int64) Item {
	var p tools.QuestionParams
	_ = json.Unmarshal([]byte(tc.Input), &p)
	var prompts []string
	for _, q := range p.Questions {
		prompts = append(prompts, q.Question)
	}
	it := Item{ID: tc.ID, Kind: "ask", Time: at, Text: strings.Join(prompts, "\n")}
	if res != nil {
		if res.IsError {
			it.Cancelled = true
		} else {
			it.Answer = strings.TrimSpace(res.Content)
		}
	}
	return it
}

// fold groups runs of foldable entries the way the TUI's tool groups do: a
// run becomes a group when it holds at least one tool call; a run of only
// thinking steps stays as it is.
func fold(entries []entry, st chatState) []Item {
	var out []Item
	var run []entry
	flush := func() {
		if len(run) == 0 {
			return
		}
		hasTools := false
		for _, e := range run {
			if !e.thinking {
				hasTools = true
				break
			}
		}
		if !hasTools {
			for _, e := range run {
				out = append(out, e.item)
			}
			run = nil
			return
		}
		g := Item{ID: "group:" + run[0].step.ID, Kind: "group", Time: run[0].step.Started}
		for _, e := range run {
			g.Steps = append(g.Steps, *e.step)
		}
		out = append(out, g)
		run = nil
	}
	for _, e := range entries {
		switch {
		case e.foldable:
			run = append(run, e)
		case e.step != nil:
			// A step that stands alone (sub-agent) is a group of one.
			flush()
			out = append(out, Item{ID: "group:" + e.step.ID, Kind: "group", Time: e.step.Started, Steps: []Step{*e.step}})
		default:
			flush()
			out = append(out, e.item)
		}
	}
	flush()

	for i := range out {
		if out[i].Kind != "group" {
			continue
		}
		live := i == len(out)-1 && st.busy
		out[i].Live = live
		out[i].Summary = groupSummary(out[i].Steps)
		if live {
			out[i].Activity = groupActivity(out[i].Steps)
		}
	}
	return out
}

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
		return "Running a command"
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
		if strings.TrimSuffix(id, ":think") == m.ID && strings.HasSuffix(id, ":think") {
			return StepDetail{ID: id, Output: m.ReasoningContent().Thinking}, true
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

// clip keeps the tail of long output: the end of a command is what matters.
func clip(s string) (string, bool) {
	s = strings.TrimRight(s, "\n")
	cut := false
	if lines := strings.Split(s, "\n"); len(lines) > detailMaxLines {
		s, cut = strings.Join(lines[len(lines)-detailMaxLines:], "\n"), true
	}
	if len(s) > detailMaxBytes {
		s, cut = s[len(s)-detailMaxBytes:], true
		for len(s) > 0 && !utf8.RuneStart(s[0]) {
			s = s[1:]
		}
	}
	return s, cut
}

// small helpers

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i]) + " …"
	}
	return s
}

func shortPath(p, wd string) string {
	if p == "" {
		return ""
	}
	if wd != "" {
		if rel, err := filepath.Rel(wd, p); err == nil && !strings.HasPrefix(rel, "..") {
			return rel
		}
	}
	return fsext.PrettyPath(p)
}

func firstStringParam(in map[string]any) string {
	for _, k := range []string{"path", "file_path", "query", "url", "name", "symbol", "command"} {
		if s, ok := in[k].(string); ok && s != "" {
			return firstLine(s)
		}
	}
	for _, v := range in {
		if s, ok := v.(string); ok && s != "" {
			return firstLine(s)
		}
	}
	return ""
}

func countLines(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return strconv.Itoa(n) + " " + word + "s"
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func between(s, a, b string) string {
	_, rest, ok := strings.Cut(s, a)
	if !ok {
		return ""
	}
	v, _, _ := strings.Cut(rest, b)
	return strings.TrimSpace(v)
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

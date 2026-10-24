package remote

import (
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/charmbracelet/crush/internal/agent"
	"github.com/charmbracelet/crush/internal/agent/tools"
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
	// Activity says what a live group is doing ("Waiting for a tool response...").
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
	// writing is set while the agent's latest step is writing a tool call
	// it hasn't sent yet.
	writing bool
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
		case message.System:
			// A notice for the user, such as a skill the agent turned on.
			if text := strings.TrimSpace(m.Content().Text); text != "" {
				entries = append(entries, entry{item: Item{ID: m.ID, Kind: "notice", Time: m.CreatedAt, Text: text, Tone: "plain"}})
			}
		}
	}
	if n := len(msgs); n > 0 {
		m := msgs[n-1]
		st.writing = st.busy && m.Role == message.Assistant && m.Activity == "writing" && !m.IsFinished() && len(m.ToolCalls()) == 0
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
			if st.writing && out[i].Activity == "Thinking" {
				out[i].Activity = "Writing command"
			}
		}
	}
	return out
}

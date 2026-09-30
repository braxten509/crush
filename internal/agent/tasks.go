package agent

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/google/uuid"

	"github.com/charmbracelet/crush/internal/agent/cliagent"
	agentprompt "github.com/charmbracelet/crush/internal/agent/prompt"
	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/charmbracelet/crush/internal/question"
	"github.com/charmbracelet/crush/internal/secureentry"
)

// Background sub-agents ("tasks"). An agent CLI running a session starts one
// by running `crush spawn` from its own shell tool, which drops a request
// file into the directory named by TasksDirEnv. Files rather than a socket
// because Codex's sandbox blocks sockets but lets commands write to the
// temp dir. Each task runs as a child session on the CLI it asked for; when
// it ends, its result is sent to the parent session as a prompt, so the
// agent picks it up mid-turn or starts a new turn for it.

const (
	// TasksDirEnv and TasksSessionEnv tell `crush spawn` where to send
	// requests and for which session.
	TasksDirEnv     = "CRUSH_TASKS_DIR"
	TasksSessionEnv = "CRUSH_SESSION_ID"

	// TaskNotificationTag wraps the result sent back to the parent.
	TaskNotificationTag = "crush-task-result"

	taskResultLimit = 30_000
)

type TaskStatus string

const (
	TaskRunning TaskStatus = "running"
	TaskDone    TaskStatus = "done"
	TaskFailed  TaskStatus = "failed"
	TaskStopped TaskStatus = "stopped"
)

// Task is a sub-agent running in the background for a session.
type Task struct {
	ID        string     `json:"id"`
	SessionID string     `json:"session_id"`
	ChildID   string     `json:"child_id"`
	Name      string     `json:"name"`
	CLI       string     `json:"cli"`
	Model     string     `json:"model"`
	Effort    string     `json:"effort,omitempty"`
	Fast      bool       `json:"fast,omitempty"`
	Status    TaskStatus `json:"status"`
	Started   time.Time  `json:"started"`
	Ended     time.Time  `json:"ended"`
}

// TaskRequest is what `crush spawn` writes; TaskReply is Crush's answer.
type TaskRequest struct {
	Session string `json:"session"`
	CLI     string `json:"cli,omitempty"`
	Model   string `json:"model,omitempty"`
	// Effort is the reasoning effort, one of the model's levels; empty
	// keeps the model's default.
	Effort string `json:"effort,omitempty"`
	// Fast runs Claude or Codex in its fast mode.
	Fast   bool   `json:"fast,omitempty"`
	Name   string `json:"name,omitempty"`
	Prompt string `json:"prompt,omitempty"`
	Stop   string `json:"stop,omitempty"`
	// Ask holds question tool input from `crush ask`.
	Ask         json.RawMessage   `json:"ask,omitempty"`
	SecureEntry *secureentry.Spec `json:"secure_entry,omitempty"`
}

type TaskReply struct {
	Task  *Task  `json:"task,omitempty"`
	Error string `json:"error,omitempty"`
}

type taskHub struct {
	c      *coordinator
	dir    string
	events pubsub.Publisher[Task]

	mu      sync.Mutex
	seq     int
	cancels map[string]context.CancelFunc
	tasks   map[string]*Task
	// userStopped marks tasks the user stopped, whose parent is told.
	userStopped map[string]bool
	// asking is set while questions from `crush ask` are open.
	asking bool
}

func newTaskHub(c *coordinator, events pubsub.Publisher[Task]) *taskHub {
	base := filepath.Join(os.TempDir(), fmt.Sprintf("crush-%d", os.Getuid()))
	if err := os.MkdirAll(base, 0o700); err != nil {
		slog.Warn("Sub-agents disabled: no request directory", "error", err)
		return nil
	}
	dir, err := os.MkdirTemp(base, "tasks-")
	if err != nil {
		slog.Warn("Sub-agents disabled: no request directory", "error", err)
		return nil
	}
	h := &taskHub{c: c, dir: dir, events: events, cancels: map[string]context.CancelFunc{}, tasks: map[string]*Task{}, userStopped: map[string]bool{}}
	go h.watch()
	hubsMu.Lock()
	hubs = append(hubs, h)
	hubsMu.Unlock()
	return h
}

// hubs are the task hubs of this process, for the UI's stop and kill
// actions.
// ponytail: package state; the UI runs in-process with the agents.
var (
	hubsMu sync.Mutex
	hubs   []*taskHub
)

// StopTask stops a background sub-agent for the user. Its parent agent is
// told, so it doesn't keep waiting for a result.
func StopTask(id string) error {
	hubsMu.Lock()
	defer hubsMu.Unlock()
	for _, h := range hubs {
		h.mu.Lock()
		t, ok := h.tasks[id]
		if ok && t.Status == TaskRunning {
			t.Status = TaskStopped
			h.userStopped[id] = true
			h.cancels[id]()
		}
		h.mu.Unlock()
		if ok {
			return nil
		}
	}
	return fmt.Errorf("no task %q", id)
}

// SessionTasks returns copies of a session's sub-agents, running or ended.
func SessionTasks(sessionID string) []Task {
	hubsMu.Lock()
	defer hubsMu.Unlock()
	var out []Task
	for _, h := range hubs {
		h.mu.Lock()
		for _, t := range h.tasks {
			if t.SessionID == sessionID {
				out = append(out, *t)
			}
		}
		h.mu.Unlock()
	}
	slices.SortFunc(out, func(a, b Task) int { return a.Started.Compare(b.Started) })
	return out
}

// env is added to the CLI process of a session that may spawn tasks.
func (h *taskHub) env(sessionID string) []string {
	return []string{TasksDirEnv + "=" + h.dir, TasksSessionEnv + "=" + sessionID}
}

// ponytail: polls the request dir; fsnotify if the latency ever matters.
func (h *taskHub) watch() {
	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	for range tick.C {
		entries, err := os.ReadDir(h.dir)
		if errors.Is(err, os.ErrNotExist) {
			return
		}
		for _, e := range entries {
			name, ok := strings.CutSuffix(e.Name(), ".req")
			if !ok {
				continue
			}
			path := filepath.Join(h.dir, e.Name())
			data, err := os.ReadFile(path)
			_ = os.Remove(path)
			if err != nil {
				continue
			}
			reply := h.handle(data)
			out, _ := json.Marshal(reply)
			tmp := filepath.Join(h.dir, name+".tmp")
			if os.WriteFile(tmp, out, 0o600) == nil {
				_ = os.Rename(tmp, filepath.Join(h.dir, name+".ack"))
			}
		}
	}
}

func (h *taskHub) handle(data []byte) TaskReply {
	var req TaskRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return TaskReply{Error: "bad request: " + err.Error()}
	}
	if req.SecureEntry != nil {
		if err := h.secureEntry(req); err != nil {
			return TaskReply{Error: err.Error()}
		}
		return TaskReply{}
	}
	if req.Ask != nil {
		if err := h.ask(req); err != nil {
			return TaskReply{Error: err.Error()}
		}
		return TaskReply{}
	}
	var t *Task
	var err error
	if req.Stop != "" {
		t, err = h.stop(req.Session, req.Stop)
	} else {
		t, err = h.spawn(req)
	}
	if err != nil {
		return TaskReply{Error: err.Error()}
	}
	return TaskReply{Task: t}
}

// cliName is the short name agents use for a CLI provider type.
func cliName(t catwalk.Type) string {
	s := strings.TrimSuffix(string(t), "-cli")
	return strings.TrimSuffix(s, "-code")
}

func taskProviderName(provider config.ProviderConfig) string {
	if provider.ID == config.AbacusProviderID {
		return provider.ID
	}
	return cliName(provider.Type)
}

// cliProviders lists the configured agent CLI providers.
func (h *taskHub) taskProviders() []config.ProviderConfig {
	var out []config.ProviderConfig
	for _, p := range h.c.cfg.Config().Providers.Seq2() {
		if (config.IsCLIProviderType(p.Type) || p.ID == config.AbacusProviderID) && !p.Disable && len(p.Models) > 0 {
			out = append(out, p)
		}
	}
	slices.SortFunc(out, func(a, b config.ProviderConfig) int { return strings.Compare(taskProviderName(a), taskProviderName(b)) })
	return out
}

func (h *taskHub) spawn(req TaskRequest) (*Task, error) {
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		return nil, errors.New("the task prompt is empty")
	}
	var provider *config.ProviderConfig
	var names []string
	for _, p := range h.taskProviders() {
		names = append(names, taskProviderName(p))
		if strings.EqualFold(req.CLI, taskProviderName(p)) || (config.IsCLIProviderType(p.Type) && strings.EqualFold(req.CLI, string(p.Type))) {
			provider = &p
		}
	}
	if provider == nil {
		return nil, fmt.Errorf("unknown CLI %q; available: %s", req.CLI, strings.Join(names, ", "))
	}
	model := provider.Models[0]
	if req.Model != "" {
		i := slices.IndexFunc(provider.Models, func(m catwalk.Model) bool { return strings.EqualFold(m.ID, req.Model) })
		if i < 0 {
			return nil, fmt.Errorf("unknown %s model %q; available: %s", taskProviderName(*provider), req.Model, strings.Join(modelIDs(provider.Models), ", "))
		}
		model = provider.Models[i]
	}

	ctx := context.Background()
	if _, err := h.c.sessions.Get(ctx, req.Session); err != nil {
		return nil, fmt.Errorf("unknown session %q", req.Session)
	}
	selected, err := taskModel(*provider, model, req)
	if err != nil {
		return nil, err
	}
	fp, err := h.c.buildProvider(*provider, selected, true)
	if err != nil {
		return nil, err
	}
	lm, err := fp.LanguageModel(ctx, model.ID)
	if err != nil {
		return nil, err
	}
	// Sub-agents work on their own, like Claude Code's, but within what
	// Crush's YOLO mode allows.
	if cm, ok := lm.(*cliagent.Model); ok {
		cm.Guarded = true
		// Marks what it starts, so leftovers show up as background
		// processes. Without a session it can't spawn sub-agents itself.
		cm.Env = []string{TasksDirEnv + "=" + h.dir}
	}
	m := Model{Model: lm, CatwalkCfg: model, ModelCfg: selected, FlatRate: provider.FlatRate}
	var subTasks *taskHub
	if !config.IsCLIProviderType(provider.Type) {
		subTasks = h
	}
	sub := NewSessionAgent(SessionAgentOptions{
		LargeModel:  m,
		SmallModel:  m,
		IsSubAgent:  true,
		IsYolo:      h.c.permissions.SkipRequests(),
		Sessions:    h.c.sessions,
		Messages:    h.c.messages,
		Cfg:         h.c.cfg,
		Tasks:       subTasks,
		Notify:      h.c.notify,
		RunComplete: h.c.runComplete,
	})
	if !config.IsCLIProviderType(provider.Type) {
		lm = newRequestTimeoutModel(lm, h.c.cfg.Config().Options.GetRequestTimeout())
		m.Model = lm
		sub.SetModels(m, m)
		agentCfg := h.c.cfg.Config().Agents[config.AgentTask]
		nativeTools, err := h.c.buildTools(ctx, agentCfg, true)
		if err != nil {
			return nil, err
		}
		sub.SetTools(nativeTools)
		p, err := taskPrompt(agentprompt.WithWorkingDir(h.c.cfg.WorkingDir()))
		if err != nil {
			return nil, err
		}
		text, err := p.Build(ctx, lm.Provider(), model.ID, h.c.cfg)
		if err != nil {
			return nil, err
		}
		sub.SetSystemPrompt(text)
	}

	name := cmp.Or(strings.TrimSpace(req.Name), firstLine(prompt, 40))
	child, err := h.c.sessions.CreateTaskSession(ctx, uuid.NewString(), req.Session, name)
	if err != nil {
		return nil, err
	}
	if !config.IsCLIProviderType(provider.Type) {
		h.c.permissions.AutoApproveSession(child.ID)
	}

	h.mu.Lock()
	h.seq++
	t := &Task{
		ID: fmt.Sprintf("t%d", h.seq), SessionID: req.Session, ChildID: child.ID, Name: name,
		CLI: taskProviderName(*provider), Model: model.ID, Effort: selected.ReasoningEffort, Fast: req.Fast, Status: TaskRunning, Started: time.Now(),
	}
	runCtx, cancel := context.WithCancel(ctx)
	h.cancels[t.ID] = cancel
	h.tasks[t.ID] = t
	snapshot := *t
	h.mu.Unlock()
	h.publish(snapshot)

	go func() {
		defer cancel()
		result, err := sub.Run(runCtx, SessionAgentCall{
			SessionID:       child.ID,
			Prompt:          subAgentPreamble + prompt,
			MaxOutputTokens: model.DefaultMaxTokens,
			NonInteractive:  true,
			ProviderOptions: getProviderOptions(m, *provider),
		})
		h.finish(t.ID, subAgentOutput(result), err)
	}()
	return &snapshot, nil
}

// taskModel is the model a sub-agent runs on, tuned as its request asks:
// the effort must be one of the model's levels, and only Claude and Codex
// have a fast mode.
func taskModel(provider config.ProviderConfig, model catwalk.Model, req TaskRequest) (config.SelectedModel, error) {
	selected := config.SelectedModel{Provider: provider.ID, Model: model.ID, ReasoningEffort: model.DefaultReasoningEffort}
	if effort := strings.ToLower(strings.TrimSpace(req.Effort)); effort != "" {
		if !slices.Contains(model.ReasoningLevels, effort) {
			if len(model.ReasoningLevels) == 0 {
				return selected, fmt.Errorf("%s model %q has no effort levels", taskProviderName(provider), model.ID)
			}
			return selected, fmt.Errorf("%s model %q takes effort %s, not %q", taskProviderName(provider), model.ID, strings.Join(model.ReasoningLevels, ", "), effort)
		}
		selected.ReasoningEffort = effort
	}
	if req.Fast {
		if provider.Type != config.TypeClaudeCode && !config.SupportsFastMode(provider, model) {
			return selected, fmt.Errorf("fast mode is only for claude and codex, not %s", cliName(provider.Type))
		}
		selected.ServiceTier = "fast"
	}
	return selected, nil
}

const subAgentPreamble = `You are a sub-agent that another AI agent started in the background from Crush. Do the task below on your own; nobody can answer questions while you work. Never ask the user anything (no "crush ask", no question tools): settle small details yourself. If the task says to check with the user first, or an open question would change the work substantially, don't guess: stop before that part and end with the questions (and the options you see) in your final report, so the agent that started you can answer them or ask the user. You end as soon as you stop replying, so finish what you start instead of leaving it running in the background. Keep waits of 10 seconds or less in the foreground. For routine commands, wait at least 10 seconds before yielding (for Codex, use yield_time_ms of at least 10000). Crush refuses a "sleep" longer than 10 seconds in the foreground; to wait for something, loop on a check (until <check>; do sleep 2; done). When you're done, end with a concise report of what you did and found: that final message is all the other agent will see.

`

// ask shows an agent's questions in Crush's question form. The answers
// reach the session like a sub-agent's result, so the agent doesn't hold
// its turn open waiting for the user.
func (h *taskHub) ask(req TaskRequest) error {
	if h.c.questions == nil {
		return errors.New("this Crush can't show questions")
	}
	var params tools.QuestionParams
	if err := json.Unmarshal(req.Ask, &params); err != nil {
		return fmt.Errorf("bad questions JSON: %w", err)
	}
	qs, err := tools.BuildQuestions(params)
	if err != nil {
		return err
	}
	r := question.Request{SessionID: req.Session, Questions: qs, ConfirmTitle: params.ConfirmTitle, ConfirmDescription: params.ConfirmDescription}
	if err := r.Validate(); err != nil {
		return err
	}
	ctx := context.Background()
	if _, err := h.c.sessions.Get(ctx, req.Session); err != nil {
		return fmt.Errorf("unknown session %q", req.Session)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.asking {
		return errors.New("other questions are still open; wait for their answers first")
	}
	h.asking = true
	go func() {
		answers, err := h.c.questions.Ask(ctx, r)
		h.mu.Lock()
		h.asking = false
		h.mu.Unlock()
		status, out := AskAnswered, ""
		switch {
		case errors.Is(err, question.ErrCancelled):
			status, out = AskCancelled, "The user closed the questions without answering."
		case err != nil:
			status, out = string(TaskFailed), "Error: "+err.Error()
		default:
			out = tools.FormatAnswers(answers, r.Questions)
		}
		msg := fmt.Sprintf("<%s>\n<name>%s</name>\n<status>%s</status>\n<result>\n%s\n</result>\n</%s>",
			TaskNotificationTag, AskName, status, out, TaskNotificationTag)
		if _, err := h.c.Run(ctx, req.Session, msg); err != nil && !errors.Is(err, context.Canceled) {
			slog.Error("Failed to hand answers to their session", "error", err)
		}
	}()
	return nil
}

// Answers from `crush ask` come back as a task result with this name and
// one of these statuses.
const (
	AskName      = "Questions"
	AskAnswered  = "answered"
	AskCancelled = "cancelled"
)

func (h *taskHub) stop(sessionID, id string) (*Task, error) {
	h.mu.Lock()
	t, ok := h.tasks[id]
	if !ok || t.SessionID != sessionID {
		h.mu.Unlock()
		return nil, fmt.Errorf("no task %q in this session", id)
	}
	if t.Status == TaskRunning {
		t.Status = TaskStopped
		h.cancels[id]()
	}
	snapshot := *t
	h.mu.Unlock()
	return &snapshot, nil
}

// stopAll ends every task as Crush shuts down, and the request directory
// with them.
func (h *taskHub) stopAll() {
	_ = os.RemoveAll(h.dir)
	h.mu.Lock()
	defer h.mu.Unlock()
	for id, t := range h.tasks {
		if t.Status == TaskRunning {
			t.Status = TaskStopped
			h.cancels[id]()
		}
	}
}

func (h *taskHub) finish(id, output string, err error) {
	h.mu.Lock()
	t := h.tasks[id]
	stopped := t.Status == TaskStopped
	tellParent := !stopped || h.userStopped[id]
	switch {
	case stopped:
	case err != nil:
		t.Status = TaskFailed
	default:
		t.Status = TaskDone
	}
	t.Ended = time.Now()
	delete(h.cancels, id)
	snapshot := *t
	h.mu.Unlock()
	h.publish(snapshot)

	ctx := context.Background()
	if err := h.c.updateParentSessionCost(ctx, snapshot.ChildID, snapshot.SessionID); err != nil {
		slog.Warn("Failed to add task cost to its parent session", "task", id, "error", err)
	}
	if !tellParent {
		return
	}
	switch {
	case stopped:
		output = "The user stopped this task before it finished."
	case err != nil:
		output = "Error: " + err.Error()
	}
	if _, err := h.c.Run(ctx, snapshot.SessionID, taskNotification(snapshot, output)); err != nil && !errors.Is(err, context.Canceled) {
		slog.Error("Failed to hand a task result to its session", "task", id, "error", err)
	}
}

// hasRunning reports whether any sub-agent of the session is still running.
func (h *taskHub) hasRunning(sessionID string) bool {
	if h == nil {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, t := range h.tasks {
		if t.SessionID == sessionID && t.Status == TaskRunning {
			return true
		}
	}
	return false
}

func (h *taskHub) publish(t Task) {
	if h.events != nil {
		h.events.Publish(pubsub.UpdatedEvent, t)
	}
}

func taskNotification(t Task, output string) string {
	output = strings.TrimSpace(output)
	if output == "" {
		output = "(no final message)"
	}
	if len(output) > taskResultLimit {
		output = output[:taskResultLimit] + "\n[result truncated]"
	}
	return fmt.Sprintf("<%s>\n<task-id>%s</task-id>\n<name>%s</name>\n<cli>%s/%s</cli>\n<status>%s</status>\n<result>\n%s\n</result>\n</%s>",
		TaskNotificationTag, t.ID, t.Name, t.CLI, t.Model, t.Status, output, TaskNotificationTag)
}

// ParseTaskNotification returns the name and status of a task result
// prompt, or ok false for anything else.
func ParseTaskNotification(text string) (name, status string, ok bool) {
	if !strings.HasPrefix(text, "<"+TaskNotificationTag+">") {
		return "", "", false
	}
	field := func(tag string) string {
		_, rest, _ := strings.Cut(text, "<"+tag+">")
		v, _, _ := strings.Cut(rest, "</"+tag+">")
		return v
	}
	return field("name"), field("status"), true
}

// instructions tell a session's agent how to use sub-agents.
func (h *taskHub) instructions() string {
	var clis strings.Builder
	for _, p := range h.taskProviders() {
		ids := modelIDs(p.Models)
		if len(ids) > 8 {
			ids = append(ids[:8], "…")
		}
		fmt.Fprintf(&clis, "- %s: %s", taskProviderName(p), strings.Join(ids, ", "))
		if levels := p.Models[0].ReasoningLevels; len(levels) > 0 {
			fmt.Fprintf(&clis, " (effort: %s)", strings.Join(levels, ", "))
		}
		clis.WriteString("\n")
	}
	bin, err := os.Executable()
	if err != nil {
		bin = "crush"
	}
	return fmt.Sprintf(`<crush_sub_agents>
You are running inside Crush, which can run sub-agents for you in the background, like Claude Code's background agents. Each sub-agent uses the chosen provider (an agent CLI or the Abacus API) in this same directory. It does not see this conversation, so give it a complete, self-contained task.

Start one with your shell tool. It returns right away with a task ID:
  %[1]s spawn --cli <cli> [--model <model>] [--effort <level>] [--fast] --name "<short title>" <<'EOF'
  <task>
  EOF

Providers and models (the first model is the default), with their effort levels:
%[2]s
--effort sets the model's reasoning effort (default: the model's own); use the level the user names, like "max". --fast turns on fast mode where supported (Claude Code, Codex, and Abacus OpenAI priority models). Abacus effort levels depend on the chosen model; recent Claude models accept low, medium, high, xhigh, max.
Sub-agents run in parallel and don't block you. Never wait, sleep or poll for them: keep working, or end your turn if you have nothing else to do. When one finishes, its result arrives as a <%[3]s> message and you continue from there. Stop one with: %[1]s spawn --stop <task-id>

Crush refuses a "sleep" longer than 10 seconds in the foreground. Keep waits of 10 seconds or less in the foreground. For routine commands, wait at least 10 seconds before yielding (for Codex, use yield_time_ms of at least 10000). Run longer waits in the background, or loop on a check for what you're waiting on (until <check>; do sleep 2; done).

Use sub-agents for independent work that can run in parallel (research, separate parts of a change, reviews, second opinions from another model). Do quick or tightly coupled work yourself. Only use them when the user asks for sub-agents, other models, or parallel work, or when the task clearly benefits.
</crush_sub_agents>

<crush_questions>
The user answers questions in Crush's question form, not in chat. Whenever you need anything from the user (a decision, a clarification, a choice between options, or they ask you to ask them something), open the form with your shell tool. Never write a question to the user in your reply text (that includes offers like "Want me to...?"), and never use your own question tool. Pass the questions as JSON on stdin; the command returns right away:
  %[1]s ask <<'EOF'
  {"questions":[{"type":"single_choice","label":"<tab label, 3 words max>","question":"<one line>","description":"<why it matters, required>","choices":[{"id":"a","label":"<choice>"},{"id":"b","label":"<choice>"}]}]}
  EOF

Types: single_choice and multi_choice (2-5 choices, each with an id and label, optional short description; the form adds a type-your-own answer and notes on its own, so never add an "Other" choice), yes_no (only for accept/reject), free_text. Every question needs a description. Ask up to %[4]d at once; several show as tabs with a review step before submitting. Then end your turn with at most one short line saying the questions are open: the answers arrive as a <%[3]s> message named %[5]q. Only one set of questions can be open at a time. Sub-agents can't ask the user. When you hand one work that needs the user's input, ask the user first and put the answers in its task. When a sub-agent's result comes back with questions, answer them yourself when you can, and ask the user only what you can't settle.
</crush_questions>

<crush_secure_entry>
For API keys, tokens, passwords, and other secrets, NEVER use questions, chat, CLI stdin, command arguments, environment variables, or your own tools to collect the value. Prepare a file with a literal %%s placeholder, then run:
  %[1]s secure-entry --file /absolute/path/to/file --label "Service API key"
Only metadata is passed to that command. It opens a masked local Crush dialog, writes the value directly to the file with 0600 permissions, and returns only saved/cancelled as a <%[3]s> named "Secure entry". End your turn after opening it. Never read, print, diff, attach, commit, or send the populated file to tools/models. The program itself reads and edits the file locally.
For multiple keys, prepare ALL placeholders before collecting any key, then open one dialog at a time for the SAME file. By default each replaces the first remaining %%s. Use --placeholder UNIQUE_MARKER or --occurrence N to select another slot; unique markers are best for multiple keys. Replacement is literal, not printf or a shell expansion. Prepare valid quoting for the intended file format. Existing keys must never be read by you to edit another slot. Secure entry is local-terminal only, not available through the phone or server clients.
</crush_secure_entry>`, bin, clis.String(), TaskNotificationTag, question.MaxQuestions, AskName)
}

func modelIDs(models []catwalk.Model) []string {
	ids := make([]string, len(models))
	for i, m := range models {
		ids[i] = m.ID
	}
	return ids
}

func firstLine(s string, n int) string {
	s, _, _ = strings.Cut(s, "\n")
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// secureEntry sends metadata to the local TUI and receives only a fixed status.
func (h *taskHub) secureEntry(req TaskRequest) error {
	ctx := context.Background()
	if _, err := h.c.sessions.Get(ctx, req.Session); err != nil {
		return errors.New("unknown session")
	}
	result, err := secureentry.Open(req.Session, *req.SecureEntry)
	if err != nil {
		return err
	}
	go func() {
		status := <-result
		msg := fmt.Sprintf("<%s>\n<name>Secure entry</name>\n<status>%s</status>\n<result>Secure entry %s. No value is returned. Do not read or print the destination file.</result>\n</%s>", TaskNotificationTag, status, status, TaskNotificationTag)
		if _, err := h.c.Run(ctx, req.Session, msg); err != nil && !errors.Is(err, context.Canceled) {
			slog.Error("Failed to deliver secure entry status")
		}
	}()
	return nil
}

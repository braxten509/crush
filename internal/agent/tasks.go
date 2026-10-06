package agent

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"charm.land/catwalk/pkg/catwalk"
	"charm.land/fantasy"
	"github.com/google/uuid"

	"github.com/charmbracelet/crush/internal/agent/cliagent"
	agentprompt "github.com/charmbracelet/crush/internal/agent/prompt"
	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/charmbracelet/crush/internal/question"
	"github.com/charmbracelet/crush/internal/secureentry"
	"github.com/charmbracelet/crush/internal/shell"
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
	ID        string `json:"id"`
	SessionID string `json:"session_id"`
	ChildID   string `json:"child_id"`
	Name      string `json:"name"`
	CLI       string `json:"cli"`
	Model     string `json:"model"`
	Effort    string `json:"effort,omitempty"`
	Fast      bool   `json:"fast,omitempty"`
	// ReadOnly runs the sub-agent where it can't change the project's
	// files. Follow-ups keep it.
	ReadOnly  bool       `json:"read_only,omitempty"`
	Status    TaskStatus `json:"status"`
	Started   time.Time  `json:"started"`
	Ended     time.Time  `json:"ended"`
	Delivered bool       `json:"delivered,omitempty"`
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
	Fast bool `json:"fast,omitempty"`
	// ReadOnly runs the sub-agent where it can't change files.
	ReadOnly bool   `json:"read_only,omitempty"`
	Name     string `json:"name,omitempty"`
	Prompt   string `json:"prompt,omitempty"`
	Stop     string `json:"stop,omitempty"`
	// Continue sends Prompt as a follow-up to this finished task.
	Continue string `json:"continue,omitempty"`
	// Ask holds question tool input from `crush ask`.
	Ask         json.RawMessage    `json:"ask,omitempty"`
	SecureEntry *secureentry.Spec  `json:"secure_entry,omitempty"`
	Background  *BackgroundRequest `json:"background,omitempty"`
}

type TaskReply struct {
	Task       *Task                 `json:"task,omitempty"`
	Error      string                `json:"error,omitempty"`
	Background *fantasy.ToolResponse `json:"background,omitempty"`
}

type taskHub struct {
	c      *coordinator
	dir    string
	root   *os.Root
	events pubsub.Publisher[Task]

	mu      sync.Mutex
	seq     int
	cancels map[string]context.CancelFunc
	tasks   map[string]*Task
	// userStopped marks tasks the user stopped, whose parent is told.
	userStopped map[string]bool
	// asking is set while questions from `crush ask` are open.
	asking bool
	// closing is set once Crush shuts down; tasks it cuts off stay saved.
	closing bool
	// resumeOnce starts the tasks an earlier Crush left unfinished.
	resumeOnce sync.Once
	// sessionRuns owns non-interactive runs and their follow-up producers.
	sessionRuns      map[string]*sessionRun
	backgroundOwners map[string]string
	backgroundShells map[string]*shell.BackgroundShell
}

func newTaskHub(c *coordinator, events pubsub.Publisher[Task]) *taskHub {
	base := filepath.Join(os.TempDir(), fmt.Sprintf("crush-%d", os.Getuid()))
	if err := privateTaskBase(base); err != nil {
		slog.Warn("Sub-agents disabled: no request directory", "error", err)
		return nil
	}
	removeStaleTaskDirectories(base)
	dir, err := os.MkdirTemp(base, fmt.Sprintf("tasks-%d-", os.Getpid()))
	if err != nil {
		slog.Warn("Sub-agents disabled: no request directory", "error", err)
		return nil
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		_ = os.Remove(dir)
		return nil
	}
	h := &taskHub{c: c, dir: dir, root: root, events: events, cancels: map[string]context.CancelFunc{}, tasks: map[string]*Task{}, userStopped: map[string]bool{}}
	go h.watch()
	go h.watchDetached()
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
	root := h.root
	if root == nil {
		var err error
		root, err = os.OpenRoot(h.dir)
		if err != nil {
			return
		}
	}
	defer root.Close()
	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	for range tick.C {
		if _, err := os.Lstat(h.dir); errors.Is(err, os.ErrNotExist) {
			return
		}
		directory, err := root.Open(".")
		if err != nil {
			return
		}
		entries, err := directory.ReadDir(-1)
		_ = directory.Close()
		if err != nil {
			continue
		}
		for _, e := range entries {
			name, ok := strings.CutSuffix(e.Name(), ".req")
			if !ok {
				continue
			}
			if !e.Type().IsRegular() {
				continue
			}
			data, err := root.ReadFile(e.Name())
			_ = root.Remove(e.Name())
			if err != nil {
				continue
			}
			// Permission prompts for background jobs must not block questions,
			// stop requests, or other jobs from reaching the hub.
			var request TaskRequest
			if json.Unmarshal(data, &request) == nil && request.Background != nil {
				go func() {
					reply := h.handle(data)
					out, _ := json.Marshal(reply)
					_ = writeTaskReply(root, name, out)
				}()
				continue
			}
			reply := h.handle(data)
			out, _ := json.Marshal(reply)
			_ = writeTaskReply(root, name, out)
		}
	}
}

func writeTaskReply(root *os.Root, name string, out []byte) error {
	tmp := ".reply-" + uuid.NewString()
	file, err := root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer root.Remove(tmp)
	_, err = file.Write(out)
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return root.Rename(tmp, name+".ack")
}

func removeStaleTaskDirectories(base string) {
	root, err := os.OpenRoot(base)
	if err != nil {
		return
	}
	defer root.Close()
	entries, err := os.ReadDir(base)
	if err != nil {
		return
	}
	for _, entry := range entries {
		suffix, ok := strings.CutPrefix(entry.Name(), "tasks-")
		owner, random, okPID := strings.Cut(suffix, "-")
		pid, err := strconv.Atoi(owner)
		if !ok || !okPID || random == "" || err != nil || pid <= 0 || !entry.IsDir() || taskProcessAlive(pid) {
			continue
		}
		info, err := entry.Info()
		if err == nil && privateTaskDirectory(info) {
			_ = root.RemoveAll(entry.Name())
		}
	}
}

func (h *taskHub) handle(data []byte) TaskReply {
	var req TaskRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return TaskReply{Error: "bad request: " + err.Error()}
	}
	if req.Background != nil {
		return h.background(req)
	}
	if req.SecureEntry != nil {
		// `crush secure-entry` is shorthand for a one-question form, so the
		// secret is typed into the same inline form as every other question.
		ask, err := secureEntryAsk(*req.SecureEntry)
		if err != nil {
			return TaskReply{Error: err.Error()}
		}
		req.Ask, req.SecureEntry = ask, nil
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
	} else if req.Continue != "" {
		t, err = h.continueTask(req)
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
	ctx, release := h.reserveFollowUp(req.Session)
	started := false
	defer func() {
		if !started {
			release()
		}
	}()
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

	if _, err := h.c.sessions.Get(ctx, req.Session); err != nil {
		return nil, fmt.Errorf("unknown session %q", req.Session)
	}
	selected, err := taskModel(*provider, model, req)
	if err != nil {
		return nil, err
	}
	sub, m, err := h.subAgent(ctx, *provider, model, selected, req.ReadOnly)
	if err != nil {
		return nil, err
	}

	name := cmp.Or(strings.TrimSpace(req.Name), firstLine(prompt, 40))
	child, err := h.c.sessions.CreateTaskSession(ctx, uuid.NewString(), req.Session, name)
	if err != nil {
		return nil, err
	}
	if !config.IsCLIProviderType(provider.Type) {
		h.c.permissions.AutoApproveSession(child.ID)
	}

	h.skipUsedIDs(req.Session)
	t := &Task{
		SessionID: req.Session, ChildID: child.ID, Name: name,
		CLI: taskProviderName(*provider), Model: model.ID, Effort: selected.ReasoningEffort, Fast: req.Fast, ReadOnly: req.ReadOnly, Status: TaskRunning, Started: time.Now(),
	}
	started = true
	snapshot := h.start(ctx, release, t, sub, m, *provider, subAgentPrompt(prompt, req.ReadOnly))
	return &snapshot, nil
}

// subAgent builds the agent a task runs on.
func (h *taskHub) subAgent(ctx context.Context, provider config.ProviderConfig, model catwalk.Model, selected config.SelectedModel, readOnly bool) (SessionAgent, Model, error) {
	fp, err := h.c.buildProvider(provider, selected, true)
	if err != nil {
		return nil, Model{}, err
	}
	lm, err := fp.LanguageModel(ctx, model.ID)
	if err != nil {
		return nil, Model{}, err
	}
	// Sub-agents work on their own, like Claude Code's, but within what
	// Crush's YOLO mode allows.
	if cm, ok := lm.(*cliagent.Model); ok {
		cm.Guarded = true
		cm.ReadOnly = readOnly
		// Marks what it starts, so leftovers show up as background
		// processes. Without a session it can't spawn sub-agents itself.
		// A read-only one isn't told where Crush is: Crush runs its
		// background jobs outside the read-only sandbox.
		if !readOnly {
			cm.Env = []string{TasksDirEnv + "=" + h.dir}
		}
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
			return nil, Model{}, err
		}
		sub.SetTools(nativeTools)
		p, err := taskPrompt(agentprompt.WithWorkingDir(h.c.cfg.WorkingDir()))
		if err != nil {
			return nil, Model{}, err
		}
		text, err := p.Build(ctx, lm.Provider(), model.ID, h.c.cfg)
		if err != nil {
			return nil, Model{}, err
		}
		sub.SetSystemPrompt(text)
	}
	return sub, m, nil
}

// start runs a task in the background and returns its first snapshot. A
// task without an ID gets the next one. release is called once the task's
// result is delivered.
func (h *taskHub) start(ctx context.Context, release func(), t *Task, sub SessionAgent, m Model, provider config.ProviderConfig, prompt string) Task {
	h.mu.Lock()
	if t.ID == "" {
		h.seq++
		t.ID = fmt.Sprintf("t%d", h.seq)
	}
	runCtx, cancel := context.WithCancel(ctx)
	h.cancels[t.ID] = cancel
	h.tasks[t.ID] = t
	if run := h.sessionRuns[t.SessionID]; run != nil {
		h.sessionRuns[t.ChildID] = run
	}
	snapshot := *t
	h.mu.Unlock()
	h.remember(snapshot, provider.ID)
	h.publish(snapshot)

	go func() {
		defer release()
		defer cancel()
		result, err := sub.Run(runCtx, SessionAgentCall{
			SessionID:       t.ChildID,
			Prompt:          prompt,
			MaxOutputTokens: m.CatwalkCfg.DefaultMaxTokens,
			NonInteractive:  true,
			ProviderOptions: getProviderOptions(m, provider),
		})
		h.finish(ctx, t.ID, subAgentOutput(result), err)
	}()
	return snapshot
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

// subAgentPrompt is a new sub-agent's first prompt.
func subAgentPrompt(prompt string, readOnly bool) string {
	if readOnly {
		return subAgentPreamble + readOnlyPreamble + prompt
	}
	return subAgentPreamble + prompt
}

// readOnlyPreamble tells a read-only sub-agent where it stands, so it
// doesn't spend its turn fighting the sandbox.
const readOnlyPreamble = `You are read-only: you can read anything and run commands, but nothing can change this project's files (writes fail with a read-only file system error). Don't try to work around that. Where a change is needed, describe it in your report instead.

`

const subAgentPreamble = `You are a sub-agent that another AI agent started in the background from Crush. Do the task below on your own; nobody can answer questions while you work. Never ask the user anything (no "crush ask", no question tools): settle small details yourself. If the task says to check with the user first, or an open question would change the work substantially, don't guess: stop before that part and end with the questions (and the options you see) in your final report, so the agent that started you can answer them or ask the user. You end as soon as you stop replying, so finish what you start instead of leaving it running in the background. Keep waits of 10 seconds or less in the foreground. For routine commands, wait at least 10 seconds before yielding (for Codex, use yield_time_ms of at least 10000). Crush refuses a "sleep" longer than 10 seconds in the foreground; to wait for something, loop on a check (until <check>; do sleep 2; done). When you're done, end with a concise report of what you did and found: that final message is all the other agent will see.

`

// ask shows an agent's questions in Crush's question form. The answers
// reach the session like a sub-agent's result, so the agent doesn't hold
// its turn open waiting for the user.
func (h *taskHub) ask(req TaskRequest) error {
	if h.c.questions == nil {
		return errors.New("this Crush can't show questions")
	}
	r, secure, err := askQuestions(req.Ask)
	if err != nil {
		return err
	}
	r.SessionID = req.Session
	r.Prepare()
	if err := r.Validate(); err != nil {
		return err
	}
	if secure != nil && !h.c.interactive {
		return errors.New("secure_entry questions need the local interactive Crush terminal, which this run doesn't have")
	}
	ctx, release := h.reserveFollowUp(req.Session)
	started := false
	defer func() {
		if !started {
			release()
		}
	}()
	if _, err := h.c.sessions.Get(ctx, req.Session); err != nil {
		return fmt.Errorf("unknown session %q", req.Session)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.asking {
		return errors.New("other questions are still open; wait for their answers first")
	}
	// Forms with secure fields go only to the local terminal, never the
	// question service that phones and servers see.
	var form *secureentry.Form
	if secure != nil {
		if form, err = secureentry.OpenForm(req.Session, r, secure); err != nil {
			return err
		}
	}
	h.asking = true
	started = true
	go func() {
		defer release()
		var answers []question.Answer
		var statuses map[string]string
		var err error
		switch {
		case form != nil:
			var result secureentry.FormResult
			select {
			case result = <-form.Result():
			case <-ctx.Done():
				err = ctx.Err()
				form.Cancel()
				result = <-form.Result()
			}
			answers, statuses = result.Answers, result.Statuses
			if err == nil && result.Cancelled {
				err = question.ErrCancelled
			}
		case h.c.interactive:
			answers, err = h.c.questions.Ask(ctx, r)
		default:
			err = question.ErrCancelled
		}
		h.mu.Lock()
		h.asking = false
		h.mu.Unlock()
		status, out := AskAnswered, ""
		switch {
		case errors.Is(err, question.ErrCancelled):
			status, out = AskCancelled, "The user closed the questions without answering."
			if !h.c.interactive {
				out = "Questions were cancelled because this non-interactive run cannot receive user answers. Continue without assuming an answer."
			}
		case err != nil:
			status, out = string(TaskFailed), "Error: "+err.Error()
		default:
			out = formatAskAnswers(answers, r.Questions)
		}
		if form != nil {
			out = strings.TrimSpace(out + "\n\n" + secureEntryStatuses(r.Questions, statuses))
		}
		msg := fmt.Sprintf("<%s>\n<name>%s</name>\n<status>%s</status>\n<result>\n%s\n</result>\n</%s>",
			TaskNotificationTag, AskName, status, out, TaskNotificationTag)
		if _, err := h.c.Run(ctx, req.Session, msg); err != nil && !errors.Is(err, context.Canceled) {
			slog.Error("Failed to hand answers to their session", "error", err)
		}
	}()
	return nil
}

// secureAskItem is the only shape a secure_entry question may have:
// metadata, never a value.
type secureAskItem struct {
	Type        string `json:"type"`
	Label       string `json:"label,omitempty"`
	Question    string `json:"question"`
	Description string `json:"description"`
	File        string `json:"file"`
	Placeholder string `json:"placeholder,omitempty"`
	Occurrence  int    `json:"occurrence,omitempty"`
}

// askQuestions turns `crush ask` input into a request, plus the destination
// of each secure_entry question by question ID (nil when there are none).
func askQuestions(data json.RawMessage) (question.Request, map[string]secureentry.Spec, error) {
	var params tools.QuestionParams
	if err := json.Unmarshal(data, &params); err != nil {
		return question.Request{}, nil, fmt.Errorf("bad questions JSON: %w", err)
	}
	// Check the ordinary questions in place, so errors keep their numbers.
	checked := params
	checked.Questions = slices.Clone(params.Questions)
	var secureAt []int
	for i, item := range checked.Questions {
		if item.Type == string(question.TypeSecureEntry) {
			secureAt = append(secureAt, i)
			checked.Questions[i].Type = string(question.TypeFreeText)
		}
	}
	qs, err := tools.BuildQuestions(checked)
	if err != nil {
		return question.Request{}, nil, err
	}
	r := question.Request{Questions: qs, ConfirmTitle: params.ConfirmTitle, ConfirmDescription: params.ConfirmDescription}
	if len(secureAt) == 0 {
		return r, nil, nil
	}
	raw, err := rawAskQuestions(data)
	if err != nil || len(raw) != len(qs) {
		return question.Request{}, nil, errors.New("bad questions JSON: questions must be an array")
	}
	specs := make(map[string]secureentry.Spec, len(secureAt))
	for _, i := range secureAt {
		var item secureAskItem
		decoder := json.NewDecoder(bytes.NewReader(raw[i]))
		decoder.DisallowUnknownFields()
		// The decoder's own error could quote the input; report fixed text.
		if decoder.Decode(&item) != nil {
			return question.Request{}, nil, fmt.Errorf("question %d: secure_entry takes only type, label, question, description, file, placeholder and occurrence; never put a value in the JSON", i+1)
		}
		if item.File == "" {
			return question.Request{}, nil, fmt.Errorf("question %d: secure_entry needs the absolute path of a prepared file in \"file\"", i+1)
		}
		id := uuid.NewString()
		qs[i] = question.Question{ID: id, Type: question.TypeSecureEntry, Label: item.Label, Text: item.Question, Description: item.Description}
		specs[id] = secureentry.Spec{File: item.File, Label: cmp.Or(item.Label, item.Question), Placeholder: item.Placeholder, Occurrence: cmp.Or(item.Occurrence, 1)}
	}
	return r, specs, nil
}

// rawAskQuestions returns each question's JSON, accepting the questions as
// an array or as a string-encoded array, like tools.QuestionParams.
func rawAskQuestions(data json.RawMessage) ([]json.RawMessage, error) {
	var envelope struct {
		Questions json.RawMessage `json:"questions"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, err
	}
	var raw []json.RawMessage
	if json.Unmarshal(envelope.Questions, &raw) == nil {
		return raw, nil
	}
	var encoded string
	if err := json.Unmarshal(envelope.Questions, &encoded); err != nil {
		return nil, err
	}
	err := json.Unmarshal([]byte(strings.TrimSpace(encoded)), &raw)
	return raw, err
}

// formatAskAnswers writes the ordinary answers for the agent. Secure
// questions are left out: only their status is reported.
func formatAskAnswers(answers []question.Answer, questions []question.Question) string {
	var ordinary []question.Question
	var ordinaryAnswers []question.Answer
	for _, q := range questions {
		if q.Type == question.TypeSecureEntry {
			continue
		}
		answer := question.Answer{QuestionID: q.ID}
		if i := slices.IndexFunc(answers, func(a question.Answer) bool { return a.QuestionID == q.ID }); i >= 0 {
			answer = answers[i]
		}
		ordinary = append(ordinary, q)
		ordinaryAnswers = append(ordinaryAnswers, answer)
	}
	if len(ordinary) == 0 {
		return ""
	}
	return tools.FormatAnswers(ordinaryAnswers, ordinary)
}

// secureEntryStatuses reports each secure question as saved or cancelled,
// the only results secure entry ever returns.
func secureEntryStatuses(questions []question.Question, statuses map[string]string) string {
	var b strings.Builder
	b.WriteString("Secure entries (no value is returned; do not read, print or send the destination files):")
	for _, q := range questions {
		if q.Type != question.TypeSecureEntry {
			continue
		}
		status := secureentry.StatusCancelled
		if statuses[q.ID] == secureentry.StatusSaved {
			status = secureentry.StatusSaved
		}
		fmt.Fprintf(&b, "\n- %s: %s", cmp.Or(q.Label, q.Text), status)
	}
	return b.String()
}

// Answers from `crush ask` come back as a task result with this name and
// one of these statuses.
const (
	AskName      = "Questions"
	AskAnswered  = "answered"
	AskCancelled = "cancelled"
)

// BackgroundProcessName names the task result telling an agent that a
// process it detached ended.
const BackgroundProcessName = "Background process"

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
// with them. Running tasks stay saved, so the next Crush resumes them.
func (h *taskHub) stopAll() {
	_ = os.RemoveAll(h.dir)
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closing = true
	var interrupted []string
	for id, t := range h.tasks {
		if t.Status == TaskRunning {
			t.Status = TaskStopped
			h.cancels[id]()
			interrupted = append(interrupted, t.ChildID)
		}
	}
	h.markInterrupted(interrupted)
	for _, run := range h.sessionRuns {
		run.cancel()
	}
}

func (h *taskHub) finish(ctx context.Context, id, output string, err error) {
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
	closing := h.closing
	h.mu.Unlock()
	h.publish(snapshot)
	if !closing {
		h.forget(snapshot.ChildID)
	}

	if err := h.c.updateParentSessionCost(ctx, snapshot.ChildID, snapshot.SessionID); err != nil {
		slog.Warn("Failed to add task cost to its parent session", "task", id, "error", err)
	}
	if !tellParent {
		h.mu.Lock()
		t.Delivered = true
		h.mu.Unlock()
		return
	}
	switch {
	case stopped:
		output = "The user stopped this task before it finished."
	case err != nil:
		output = "Error: " + err.Error()
	}
	if _, err := h.c.Run(ctx, snapshot.SessionID, taskNotification(snapshot, output)); err != nil {
		if !errors.Is(err, context.Canceled) {
			slog.Error("Failed to hand a task result to its session", "task", id, "error", err)
		}
		return
	}
	h.mu.Lock()
	t.Delivered = true
	h.mu.Unlock()
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
	providers := h.taskProviders()
	slices.SortFunc(providers, func(a, b config.ProviderConfig) int { return strings.Compare(taskProviderName(a), taskProviderName(b)) })
	for _, p := range providers {
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
  %[1]s spawn --cli <cli> [--model <model>] [--effort <level>] [--fast] [--read-only] --name "<short title>" <<'EOF'
  <task>
  EOF

Providers and models (the first model is the default), with their effort levels:
%[2]s
--effort sets the model's reasoning effort (default: the model's own); use the level the user names, like "max". --fast turns on fast mode where supported (Claude Code, Codex, and Abacus OpenAI priority models). Abacus effort levels depend on the chosen model; recent Claude models accept low, medium, high, xhigh, max. --read-only runs the sub-agent where it can't change the project's files; use it for reviews, critiques and anything else that must only look. Run every review or second opinion from another model as a sub-agent like this, never through another tool or script that starts an agent CLI.
Sub-agents run in parallel and don't block you. Never wait, sleep or poll for them: keep working, or end your turn if you have nothing else to do. When one finishes, its result arrives as a <%[3]s> message and you continue from there. Stop one with: %[1]s spawn --stop <task-id>
To give a finished sub-agent more work or corrections, continue it instead of starting a new one; it keeps its conversation and remembers its earlier work, and it shows in Crush as the same sub-agent:
  %[1]s spawn --continue <task-id> <<'EOF'
  <follow-up>
  EOF
Task IDs keep working after Crush restarts. Never start, resume or continue an agent CLI yourself (claude, codex, grok, opencode, agy) through bg, systemd-run, scripts or its own resume flags: Crush shows those as plain background processes and cannot report their results.

Crush refuses a "sleep" longer than 10 seconds in the foreground. Keep waits of 10 seconds or less in the foreground. For routine commands, wait at least 10 seconds before yielding (for Codex, use yield_time_ms of at least 10000). Run longer waits in the background, or loop on a check for what you're waiting on (until <check>; do sleep 2; done).
Always run tests (test suites, test scripts, builds run to test) in the background, never in the foreground, using %[1]s bg -- 'timeout 600 <command>' for agent CLIs. This registers the job with Crush, makes it visible, and delivers a completion message. Use %[1]s bg --output <job-id> for output or %[1]s bg --stop <job-id> to stop it. Do not use nohup or a bare trailing ampersand for tracked jobs. Native API agents can use the bash tool's run_in_background option. Give every test command a time limit so a hung test ends on its own (for example "timeout 600 <command>", or the runner's own flag like "go test -timeout 10m"). Keep working while they run; check their output when you need the results instead of blocking on them, and if your tool tells you when a background command finishes, you can end your turn and continue then.

Use sub-agents for independent work that can run in parallel (research, separate parts of a change, reviews, second opinions from another model). Do quick or tightly coupled work yourself. Only use them when the user asks for sub-agents, other models, or parallel work, or when the task clearly benefits.
</crush_sub_agents>

<crush_questions>
The user answers questions in Crush's question form, not in chat. Whenever you need anything from the user (a decision, a clarification, a choice between options, or they ask you to ask them something), open the form with your shell tool. Never write a question to the user in your reply text (that includes offers like "Want me to...?"), and never use your own question tool. Pass the questions as JSON on stdin; the command returns right away:
  %[1]s ask <<'EOF'
  {"questions":[{"type":"single_choice","label":"<tab label, 3 words max>","question":"<one line>","description":"<why it matters, required>","choices":[{"id":"a","label":"<choice>"},{"id":"b","label":"<choice>"}]}]}
  EOF

Types: single_choice and multi_choice (2-5 choices, each with an id and label, optional short description and optional "image": an absolute path to a PNG sketch the form shows for the choice under the cursor, as the design-preview skill makes; the form adds a type-your-own answer and notes on its own, so never add an "Other" choice), yes_no (only for accept/reject), free_text, secure_entry (a masked secret field; see crush_secure_entry). Every question needs a description. Ask up to %[4]d at once; several show as tabs with a review step before submitting. Then end your turn with at most one short line saying the questions are open: the answers arrive as a <%[3]s> message named %[5]q. Only one set of questions can be open at a time. Sub-agents can't ask the user. When you hand one work that needs the user's input, ask the user first and put the answers in its task. When a sub-agent's result comes back with questions, answer them yourself when you can, and ask the user only what you can't settle.
</crush_questions>

<crush_secure_entry>
For API keys, tokens, passwords, and other secrets, NEVER use ordinary questions (free_text and the like), chat, CLI stdin, command arguments, environment variables, or your own tools to collect the value. Prepare a file with a literal placeholder for each secret first.
When you also have other questions, put the secrets in the same %[1]s ask form as secure_entry questions instead of opening a separate secure entry. They show as masked tabs next to the others, with one review and one Submit:
  {"type":"secure_entry","label":"API key","question":"Enter the service key","description":"Used by the service","file":"/absolute/path/to/file","placeholder":"KEY_SLOT","occurrence":1}
That JSON is metadata only: never include a value. placeholder defaults to %%s and occurrence to 1, counted in the file as you prepared it. Give every secret in one file its own placeholder (or occurrence). Values are written only when the user submits the whole form; the answers message has the ordinary answers plus each secure entry as only saved or cancelled.
For secrets alone, run:
  %[1]s secure-entry --file /absolute/path/to/file --label "Service API key"
That is shorthand for a one-question form with a secure_entry question: same form, same "Questions" result with only saved/cancelled. End your turn after opening it. Never read, print, diff, attach, commit, or send the populated file to tools/models. The program itself reads and edits the file locally.
For multiple keys, prepare ALL placeholders before collecting any key, then ask for them together as several secure_entry questions in one form. By default each replaces the first remaining %%s. Use unique placeholders (or occurrence) to select a slot; unique markers are best. Replacement is literal, not printf or a shell expansion. Prepare valid quoting for the intended file format. Existing keys must never be read by you to edit another slot. Secure entry is local-terminal only, not available through the phone or server clients.
Keep secrets in a secrets folder such as ~/.config/secrets. Crush blocks tool calls that read secrets folders, so when a task needs a secret, write a small helper program that reads the file itself and never prints it.
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

// secureEntryAsk turns `crush secure-entry` flags into the input of a
// one-question form, so the secret goes through the same inline form as
// secure_entry questions from `crush ask`.
func secureEntryAsk(spec secureentry.Spec) (json.RawMessage, error) {
	label := cmp.Or(strings.TrimSpace(spec.Label), "Secret")
	item := secureAskItem{
		Type:        string(question.TypeSecureEntry),
		Label:       label,
		Question:    "Enter the " + label,
		Description: "Typed into a masked field and saved straight into a local file. The agent never sees it.",
		File:        spec.File,
		Placeholder: spec.Placeholder,
		Occurrence:  spec.Occurrence,
	}
	return json.Marshal(map[string][]secureAskItem{"questions": {item}})
}

package agent

import (
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
	"testing"
	"time"

	"charm.land/catwalk/pkg/catwalk"
	"charm.land/fantasy"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/openchats"
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
	Account   string `json:"account,omitempty"`
	Effort    string `json:"effort,omitempty"`
	Fast      bool   `json:"fast,omitempty"`
	// ReadOnly runs the sub-agent where it can't change the project's
	// files. Follow-ups keep it.
	ReadOnly  bool       `json:"read_only,omitempty"`
	Status    TaskStatus `json:"status"`
	Started   time.Time  `json:"started"`
	Ended     time.Time  `json:"ended"`
	Delivered bool       `json:"delivered,omitempty"`
	// StartedBy is the client ID of the app that owns the helper (see
	// SetWorkStartHook); "" when no app does.
	StartedBy string `json:"started_by,omitempty"`
	// delivering covers the handoff to the parent, including work before
	// Run accepts it. Unlike Delivered, it clears even if delivery fails.
	delivering bool
	// awaitingHandoff covers only the gap before the result's turn is
	// accepted, so a turn ending in that gap doesn't ding as if all work
	// were done. The result's own turn may still ding.
	awaitingHandoff bool
	// chat, set for a follow-up the user typed in the task's chat, gives
	// its first turn the app's prompt IDs.
	chat *chatCall
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
	// Skill turns a skill on or off for the whole chat (`crush skill`).
	Skill *SkillRequest `json:"skill,omitempty"`
	// chat marks a follow-up the user typed in the task's own chat.
	chat *chatCall
}

type TaskReply struct {
	Task       *Task                 `json:"task,omitempty"`
	Error      string                `json:"error,omitempty"`
	Background *fantasy.ToolResponse `json:"background,omitempty"`
	// Message is a plain answer for the agent, such as from `crush skill`.
	Message string `json:"message,omitempty"`
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
	// askSession is the session whose questions are open or whose answers
	// have not been handed to it yet; it holds back completion alerts.
	askSession string
	// closing is set once Crush shuts down; tasks it cuts off stay saved.
	closing bool
	// interrupting marks the tasks and jobs (by task ID and job ID) that
	// InterruptTask and InterruptJob ended: like closing, their end is saved
	// for a later resume instead of handed to the agent.
	interrupting map[string]bool
	// interruptedDetached marks the detached processes InterruptJob
	// killed, so their end is not reported to the agent.
	interruptedDetached map[detachedKey]bool
	// resumeMu runs one resume of the tasks other hubs left unfinished at
	// a time; every resume claims again.
	resumeMu sync.Mutex
	// sessionRuns owns non-interactive runs and their follow-up producers.
	sessionRuns        map[string]*sessionRun
	backgroundOwners   map[string]string
	backgroundServices map[string]bool
	backgroundShells   map[string]*shell.BackgroundShell
	// jobHandoffs marks jobs (by call ID, not services) whose result has
	// not been handed to the agent yet.
	jobHandoffs map[string]bool
	// checkIns holds each session's one check-in loop; see addCheckIn.
	checkIns   map[string]*sessionCheckIns
	checkInSeq int
	// runs holds what running tasks' chat messages need (tasks_chat.go).
	runs map[string]*taskRun
	// subAgentHook lets tests build the agent a task runs on.
	subAgentHook func(config.ProviderConfig, catwalk.Model) SessionAgent
	// checkInEvery lets tests shorten check-ins; zero uses the setting.
	checkInEvery time.Duration
	// queueStop ends the loop that saves the prompts waiting behind a
	// chat's turn (queueStopOnce, since stopAll runs more than once);
	// queueSaved is the last queue written, so an unchanged one is not
	// written again.
	queueStop     chan struct{}
	queueStopOnce sync.Once
	queueMu       sync.Mutex
	queueSaved    string
	// reaper is the write end of the reaper's pipe; see startReaper.
	reaper *os.File
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
	h := &taskHub{c: c, dir: dir, root: root, events: events, cancels: map[string]context.CancelFunc{}, tasks: map[string]*Task{}, userStopped: map[string]bool{}, queueStop: make(chan struct{})}
	if !testing.Testing() {
		if h.reaper, err = startReaper(TasksDirEnv + "=" + dir); err != nil {
			slog.Warn("Processes may outlive a crash: no reaper", "error", err)
		}
	}
	go h.watchQueues()
	go h.watch()
	go h.watchDetached()
	if !testing.Testing() {
		// Tests call syncStrays themselves.
		go h.watchStrays()
	}
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

// ErrTaskNotInSession is StopSessionTask's answer for a helper that is not
// in the chat.
var ErrTaskNotInSession = errors.New("no such helper in this chat")

// StopSessionTask is StopTask limited to one chat: same user-stop meaning
// (the parent is told). It returns ErrTaskNotInSession when the helper is
// not in sessionID. A helper that already ended is left as it is.
func StopSessionTask(sessionID, id string) error {
	hubsMu.Lock()
	defer hubsMu.Unlock()
	for _, h := range hubs {
		h.mu.Lock()
		t, ok := h.tasks[id]
		ok = ok && t.SessionID == sessionID
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
	return ErrTaskNotInSession
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

// SessionHasUnfinishedWork keeps the UI waiting through helper result
// delivery as well as execution. This is deliberately broader than the
// notification gate: a parent's final response can ding before its caller
// records that delivery finished. Services do not hold the UI waiting.
func SessionHasUnfinishedWork(sessionID string) bool {
	if sessionID == "" {
		return false
	}
	hubsMu.Lock()
	defer hubsMu.Unlock()
	for _, h := range hubs {
		if h.hasBackgroundWork(sessionID, true) {
			return true
		}
	}
	return false
}

// SessionHasRunningWork is the completion-alert gate: sub-agents or
// background jobs (not services) still running, or ended with their result
// not yet handed to the agent, or commands the agent CLI backgrounded itself.
func SessionHasRunningWork(sessionID string) bool {
	if sessionID == "" {
		return false
	}
	hubsMu.Lock()
	for _, h := range hubs {
		if h.hasRunning(sessionID) {
			hubsMu.Unlock()
			return true
		}
	}
	hubsMu.Unlock()
	return cliBackgroundRunning(sessionID)
}

// cliBackgroundRunning reports whether the session's agent CLI still runs a
// command it moved to the background with its own tools rather than
// `crush bg`, such as Claude Code's run_in_background. Crush lists those as
// background processes, so they hold back the completion alert too. Crush's
// own jobs are listed with their session as well, but hasRunning judges
// those, leaving services out. It takes hubsMu, so callers must not hold it.
func cliBackgroundRunning(sessionID string) bool {
	for _, p := range BackgroundProcesses() {
		if p.JobID == "" && p.Session == sessionID {
			return true
		}
	}
	return false
}

// env is added to the CLI process of a session that may spawn tasks.
func (h *taskHub) env(sessionID string) []string {
	return []string{TasksDirEnv + "=" + h.dir, TasksSessionEnv + "=" + sessionID}
}

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

// stopAll ends every task as its Crush, or its project's workspace, shuts
// down, and the request directory with them. Running tasks stay saved and
// released, so the next resume, in this process or the next Crush, starts
// them again. It is safe to call twice.
func (h *taskHub) stopAll() {
	// Unlisted first: from here on no resume, stop or scan finds the hub.
	// hubsMu is never held together with h.mu here.
	hubsMu.Lock()
	hubs = slices.DeleteFunc(hubs, func(other *taskHub) bool { return other == h })
	hubsMu.Unlock()
	_ = os.RemoveAll(h.dir)
	h.mu.Lock()
	h.closing = true
	var interrupted []string
	for id, t := range h.tasks {
		if t.Status == TaskRunning {
			t.Status = TaskStopped
			h.cancels[id]()
			interrupted = append(interrupted, t.ChildID)
		}
	}
	h.releaseInterrupted(interrupted)
	for _, run := range h.sessionRuns {
		run.cancel()
	}
	reaper := h.reaper
	h.reaper = nil
	h.mu.Unlock()
	// The last look at the queues, then the loop ends (a hub tests build by
	// hand has none).
	h.writeQueues()
	if h.queueStop != nil {
		h.queueStopOnce.Do(func() { close(h.queueStop) })
	}
	// Closing the pipe has the reaper stop what the hub's agents left.
	if reaper != nil {
		_ = reaper.Close()
	}
}

// finish is finishRun for the task the hub lists as id; an unknown id does
// nothing.
func (h *taskHub) finish(ctx context.Context, id, output string, err error) {
	h.mu.Lock()
	t := h.tasks[id]
	h.mu.Unlock()
	if t == nil {
		return
	}
	h.finishRun(ctx, t, output, err)
}

// finishRun ends the run of task t and hands its result to the parent. A run
// that shutdown or InterruptTask cut off stays saved, and its parent is not
// told. A run whose ID a resumed run of the same task took over meanwhile
// does nothing: the resumed run is the one shown and awaited.
func (h *taskHub) finishRun(ctx context.Context, t *Task, output string, err error) {
	h.mu.Lock()
	id := t.ID
	if h.tasks[id] != t {
		h.mu.Unlock()
		return
	}
	stopped := t.Status == TaskStopped
	interrupted := h.interrupting[id]
	delete(h.interrupting, id)
	tellParent := !interrupted && (!stopped || h.userStopped[id])
	switch {
	case stopped:
	case err != nil:
		t.Status = TaskFailed
	default:
		t.Status = TaskDone
	}
	t.Ended = time.Now()
	t.delivering = tellParent
	t.awaitingHandoff = tellParent
	delete(h.cancels, id)
	snapshot := *t
	closing := h.closing || interrupted
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		t.delivering = false
		t.awaitingHandoff = false
		h.mu.Unlock()
	}()
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
	accept := h.acceptHandoff(snapshot.SessionID, func() { t.awaitingHandoff = false })
	defer accept.Close()
	if _, err := h.c.RunAccepted(ctx, accept, snapshot.SessionID, taskNotification(snapshot, output)); err != nil {
		if !errors.Is(err, context.Canceled) {
			slog.Error("Failed to hand a task result to its session", "task", id, "error", err)
		}
		return
	}
	h.mu.Lock()
	t.Delivered = true
	h.mu.Unlock()
}

// HasRunningWork reports whether c is running anything that would be cut
// off if its workspace closed now: a reply, a sub-agent, a background job
// (not a service), or open questions.
func HasRunningWork(c Coordinator) bool {
	if c == nil {
		return false
	}
	if c.IsBusy() {
		return true
	}
	hubsMu.Lock()
	defer hubsMu.Unlock()
	for _, h := range hubs {
		if Coordinator(h.c) == c && h.hasAnyWork() {
			return true
		}
	}
	return false
}

func (h *taskHub) hasAnyWork() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.asking || h.askSession != "" {
		return true
	}
	for _, t := range h.tasks {
		if t.Status == TaskRunning || t.awaitingHandoff || t.delivering {
			return true
		}
	}
	for _, job := range h.backgroundShells {
		if !h.backgroundServices[job.ID] && !job.IsDone() {
			return true
		}
	}
	return false
}

// hasRunning reports whether any sub-agent or managed background command
// belonging to this session is still running. Explicit services stay visible
// and manageable but do not hold back completion notifications.
func (h *taskHub) hasRunning(sessionID string) bool {
	return h.hasBackgroundWork(sessionID, false)
}

func (h *taskHub) hasBackgroundWork(sessionID string, includeDelivery bool) bool {
	if h == nil {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.askSession != "" && h.askSession == sessionID {
		return true
	}
	for _, t := range h.tasks {
		if t.SessionID == sessionID && (t.Status == TaskRunning || t.awaitingHandoff || includeDelivery && t.delivering) {
			return true
		}
	}
	for callID, job := range h.backgroundShells {
		if h.backgroundOwners[job.ID] == sessionID && !h.backgroundServices[job.ID] && (includeDelivery || !job.IsDone() || h.jobHandoffs[callID]) {
			return true
		}
	}
	return false
}

// acceptHandoff reserves the result's turn with the agent before clearing
// the caller's awaiting-handoff mark, so a completion alert sees one or the
// other the whole time. The caller closes the reservation.
func (h *taskHub) acceptHandoff(sessionID string, clear func()) *AcceptedRun {
	accept := h.c.BeginAccepted(sessionID)
	h.mu.Lock()
	clear()
	h.mu.Unlock()
	return accept
}

func (h *taskHub) publish(t Task) {
	if h.events != nil {
		h.events.Publish(pubsub.UpdatedEvent, t)
	}
}

// taskResult is a <crush-task-result> message: how Crush tells an agent
// about its sub-agents, background jobs, question forms and check-ins.
// ID and CLI are left out when empty.
type taskResult struct {
	ID, Name, CLI, Status, Body string
}

func (r taskResult) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "<%s>\n", TaskNotificationTag)
	if r.ID != "" {
		fmt.Fprintf(&b, "<task-id>%s</task-id>\n", r.ID)
	}
	fmt.Fprintf(&b, "<name>%s</name>\n", r.Name)
	if r.CLI != "" {
		fmt.Fprintf(&b, "<cli>%s</cli>\n", r.CLI)
	}
	fmt.Fprintf(&b, "<status>%s</status>\n<result>\n%s\n</result>\n</%s>", r.Status, r.Body, TaskNotificationTag)
	return b.String()
}

func taskNotification(t Task, output string) string {
	output = strings.TrimSpace(output)
	if output == "" {
		output = "(no final message)"
	}
	if len(output) > taskResultLimit {
		output = output[:taskResultLimit] + "\n[result truncated]"
	}
	return taskResult{ID: t.ID, Name: t.Name, CLI: t.CLI + "/" + t.Model, Status: string(t.Status), Body: output}.String()
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

// userSubAgentInstructions returns the user's own rules for starting
// sub-agents, saved from the Sub-agent Instructions command, ready to close
// the sub-agent block; "" when there are none. They are read on every call,
// so an edit reaches open chats on their next message.
func userSubAgentInstructions() string {
	text := config.LoadSubAgentInstructions()
	if text == "" {
		return ""
	}
	return "\n\nThe user's own instructions for starting sub-agents follow. They decide when to use sub-agents and which provider, model, effort and options each gets; they take precedence over the guidance above and over any memory note or skill that says otherwise:\n<user_sub_agent_instructions>\n" + text + "\n</user_sub_agent_instructions>"
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
		if !config.IsCLIProviderType(p.Type) {
			clis.WriteString(" [API]")
		}
		clis.WriteString("\n")
	}
	bin, err := os.Executable()
	if err != nil {
		bin = "crush"
	}
	return fmt.Sprintf(`<crush_sub_agents>
You are running inside Crush, which can run sub-agents for you in the background, like Claude Code's background agents. Each sub-agent uses the chosen provider (an agent CLI or an API service) in this same directory. It does not see this conversation, so give it a complete, self-contained task.

Start one with your shell tool. It returns right away with a task ID:
  %[1]s spawn --cli <cli> [--model <model>] [--effort <level>] [--fast] [--read-only] --name "<short title>" <<'EOF'
  <task>
  EOF

Providers and models (the first model is the default), with their effort levels:
%[2]s
Providers marked [API] are API services billed per use: never start a sub-agent on one unless the user asks for that provider or one of its models.
--effort sets the model's reasoning effort (default: the model's own); use the level the user names, like "max". --fast turns on fast mode where supported (Claude Code, Codex, and Abacus OpenAI priority models). Abacus effort levels depend on the chosen model; recent Claude models accept low, medium, high, xhigh, max. --read-only runs the sub-agent where it can't change the project's files; use it for reviews, critiques and anything else that must only look. Run every review or second opinion from another model as a sub-agent like this, never through another tool or script that starts an agent CLI.
Sub-agents run in parallel and don't block you. Never wait, sleep or poll for them: keep working, or end your turn if you have nothing else to do. When one finishes, its result arrives as a <%[3]s> message and you continue from there.%[6]s Stop one with: %[1]s spawn --stop <task-id>
To give a finished sub-agent more work or corrections, continue it instead of starting a new one; it keeps its conversation and remembers its earlier work, and it shows in Crush as the same sub-agent:
  %[1]s spawn --continue <task-id> <<'EOF'
  <follow-up>
  EOF
Task IDs keep working after Crush restarts. Never start, resume or continue an agent CLI yourself (claude, codex, grok, opencode, agy) through bg, systemd-run, scripts or its own resume flags: Crush shows those as plain background processes and cannot report their results.

Crush refuses a "sleep" longer than 10 seconds in the foreground. Keep waits of 10 seconds or less in the foreground. For routine commands, wait at least 10 seconds before yielding (for Codex, use yield_time_ms of at least 10000). Run longer waits in the background, or loop on a check for what you're waiting on (until <check>; do sleep 2; done).
Always run tests (test suites, test scripts, builds run to test) in the background, never in the foreground, using %[1]s bg -- 'timeout 600 <command>' for agent CLIs. This registers the job with Crush, makes it visible, and delivers a completion message. Use %[1]s bg --output <job-id> for output or %[1]s bg --stop <job-id> to stop it. Do not use nohup or a bare trailing ampersand for tracked jobs. Native API agents can use the bash tool's run_in_background option. Give every test command a time limit so a hung test ends on its own (for example "timeout 600 <command>", or the runner's own flag like "go test -timeout 10m"). Keep working while they run; check their output when you need the results instead of blocking on them, and if your tool tells you when a background command finishes, you can end your turn and continue then.
Use %[1]s bg --service -- '<command>' for servers and watchers that should remain running after the work is complete. This keeps them tracked and stoppable without delaying the finish notification. Never mark tests, builds, or other finite work as services.

Use sub-agents for independent work that can run in parallel (research, separate parts of a change, reviews, second opinions from another model). Do quick or tightly coupled work yourself. Only use them when the user asks for sub-agents, other models, or parallel work, or when the task clearly benefits.%[7]s
</crush_sub_agents>

<crush_questions>
The user answers questions in Crush's question form, not in chat. Whenever you need anything from the user (a decision, a clarification, a choice between options, or they ask you to ask them something), open the form with your shell tool. Never write a question to the user in your reply text (that includes offers like "Want me to...?"), and never use your own question tool. Pass the questions as JSON on stdin; the command returns right away:
  %[1]s ask <<'EOF'
  {"questions":[{"type":"single_choice","label":"<tab label, 3 words max>","question":"<one line>","description":"<why it matters, required>","choices":[{"id":"a","label":"<choice>"},{"id":"b","label":"<choice>"}]}]}
  EOF

Types: single_choice and multi_choice (2-5 choices, each with an id and label, optional short description and optional "image": an absolute path to a PNG sketch the form shows for the choice under the cursor, as the design-preview skill makes; the form adds a type-your-own answer and notes on its own, so never add an "Other" choice), yes_no (only for accept/reject), free_text, secure_entry (a masked secret field; see crush_secure_entry). Every question needs a description. Ask up to %[4]d at once; several show as tabs with a review step before submitting. Then end your turn with at most one short line saying the questions are open: the answers arrive as a <%[3]s> message named %[5]q. Only one set of questions can be open at a time. Sub-agents can't ask the user. When you hand one work that needs the user's input, ask the user first and put the answers in its task. When a sub-agent's result comes back with questions, answer them yourself when you can, and ask the user only what you can't settle.
</crush_questions>

<crush_chat_skills>
Skills can be on for the whole chat: Crush then gives you their text every turn, and they survive compaction. Turn one on yourself when the chat's ongoing work fits it (not for a single quick step), and off when the work has moved on:
  %[1]s skill on <name>
  %[1]s skill off <name>
Use names from your skills list. The user sees a notice in the chat for each change. You can turn on any skill that is off, even one the user turned off, if the work needs it. You can turn off only toggleable skills: the ones you turned on and the ones the user marked toggleable. Skills the user set to plain on stay until the user turns them off. Sub-agents can't change chat skills.
</crush_chat_skills>

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
</crush_secure_entry>`, bin, clis.String(), TaskNotificationTag, question.MaxQuestions, AskName, h.checkInInstructions(), userSubAgentInstructions()) + richReplies()
}

// richReplies tells the agent it may draw diagrams and math while Crush
// Desktop runs (owner, 2026-10-21: "only while the desktop app is open"); the
// terminal shows them as plain source. Read on every call, so the note comes
// and goes with the app.
func richReplies() string {
	if list, err := openchats.Open(); err != nil || !list.DesktopOpen() {
		return ""
	}
	return `

<crush_rich_replies>
The user reads this chat in Crush Desktop, which draws Mermaid diagrams and LaTeX math in your replies. Use them when a picture or a formula explains better than words:
- Diagrams: a fenced code block with the language mermaid (flowchart, sequence, state, class, ER, Gantt, mindmap and the other Mermaid kinds).
- Math: $...$ or \(...\) inline; $$...$$ or \[...\] on their own lines for display math. A lone $ before a number stays a dollar sign.
Put them in your reply text, not in files. The terminal Crush shows them as plain source, so keep the words around them meaningful on their own.
</crush_rich_replies>`
}

func firstLine(s string, n int) string {
	s, _, _ = strings.Cut(s, "\n")
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// The tray's words as pure rules: the queue rows (TYPE-044, TYPE-045) and the
// to-do header and list (SIDE-031 to SIDE-034). A port of
// internal/ui/model/pills.go, queue_display.go and internal/ui/chat/todos.go.

// agent.TaskNotificationTag and the ask task's names (internal/agent/tasks.go:41,49;
// tasks_ask.go:24-26).
const TASK_TAG = "crush-task-result";
const TASK_RUNNING = "running";
const ASK_NAME = "Questions";
const ASK_DONE = new Set(["answered", "cancelled"]);

export const FIRST_ROW_TAG = "waiting · sends next";

// agent.ParseTaskNotification (tasks.go:538-548).
export function parseTaskNotification(text) {
  if (!String(text).startsWith(`<${TASK_TAG}>`)) return null;
  const field = (tag) => {
    const at = text.indexOf(`<${tag}>`);
    const rest = at > -1 ? text.slice(at + tag.length + 2) : "";
    const end = rest.indexOf(`</${tag}>`);
    return end > -1 ? rest.slice(0, end) : rest;
  };
  return { name: field("name"), status: field("status") };
}

// One queued prompt's row text (queueRows, pills.go:87-119): task results get
// a short label, and the text is one line.
export function queueLabel(text) {
  let label = String(text ?? "");
  const task = parseTaskNotification(label);
  if (task) {
    label = `Result of task ${task.name}`;
    if (task.status === TASK_RUNNING) label = `Progress of ${task.name}`;
    if (task.name === ASK_NAME && ASK_DONE.has(task.status)) label = "Your answers to the questions";
  }
  return label.split(/\s+/u).filter(Boolean).join(" ");
}

// The rows: the first says it is waiting and goes next.
export function queueRows(texts) {
  return texts.map((text, index) => ({ text: queueLabel(text), tag: index === 0 ? FIRST_ROW_TAG : "" }));
}

// visiblePromptQueueItems (queue_display.go:7-58): the engine's queued prompts
// that the timeline does not already show (by submission id), then this
// window's held prompts the engine does not list yet (unlistedHeld,
// prompt_queue.go:124-135). entries: [{prompt, submission_id}]; shownIds: the
// submission ids of the chat's shown user messages; held: [{text, submissionId}].
// The terminal also matches summaries without ids by text, for older engines;
// this engine always sends the ids.
export function visibleQueue({ entries = [], shownIds = new Set(), held = [] }) {
  const listed = new Set(entries.map((entry) => entry.submission_id).filter(Boolean));
  const result = entries.filter((entry) => !(entry.submission_id && shownIds.has(entry.submission_id))).map((entry) => entry.prompt ?? "");
  for (const item of held) if (!listed.has(item.submissionId)) result.push(item.text);
  return result;
}

// session.HasIncompleteTodos (internal/session/session.go:42-49).
export function hasIncomplete(todos) {
  return (todos ?? []).some((todo) => todo.status !== "completed");
}

// {done, total, active}: active is the first step in progress.
export function progress(todos) {
  const list = todos ?? [];
  return {
    done: list.filter((todo) => todo.status === "completed").length,
    total: list.length,
    active: list.find((todo) => todo.status === "in_progress") ?? null,
  };
}

// A step's words: the active form while in progress (todos.go:162-165).
export function stepText(todo) {
  return todo.status === "in_progress" && todo.active_form ? todo.active_form : todo.content;
}

// todoHeader (pills.go:47-80): "TO-DO" and "done/total"; closed, the step in
// progress; the hint at the right.
export function todoHeader(todos, open) {
  const { done, total, active } = progress(todos);
  return {
    label: "TO-DO",
    count: `${done}/${total}`,
    step: !open && active ? stepText(active) : "",
    hint: open ? "ctrl+t hide" : "ctrl+t show",
  };
}

const ORDER = { completed: 0, in_progress: 1 };

// The open list (FormatTodosList and sortTodos, todos.go:139-192): done, in
// progress, then pending, each group in its own order.
export function todoList(todos) {
  return [...(todos ?? [])]
    .map((todo, index) => ({ todo, index }))
    .sort((a, b) => (ORDER[a.todo.status] ?? 2) - (ORDER[b.todo.status] ?? 2) || a.index - b.index)
    .map(({ todo }) => ({ status: todo.status === "completed" || todo.status === "in_progress" ? todo.status : "pending", text: stepText(todo) }));
}

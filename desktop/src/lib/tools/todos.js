// The To-Do tool's header and body (CHAT-071). Port of chat/todos.go:40-192.
import { decode } from "./go.js";

const TODO = { content: "string", status: "string", active_form: "string" };
export const TODOS_PARAMS = { todos: ["array", TODO] };
const TODOS_META = {
  is_new: "bool",
  todos: ["array", TODO],
  just_completed: ["array", "string"],
  just_started: "string",
  completed: "int",
  total: "int",
};

const ORDER = { completed: 0, in_progress: 1 };
const ICONS = { completed: "✓", in_progress: "→" };

// FormatTodosList: completed, then in progress, then pending (a stable
// sort), each with its mark; an in-progress item shows its active form.
export function todoList(todos) {
  const sorted = [...(todos ?? [])].sort((a, b) => (ORDER[a.status] ?? 2) - (ORDER[b.status] ?? 2));
  return sorted.map((todo) => ({
    status: todo.status,
    icon: ICONS[todo.status] ?? "•",
    text: todo.status === "in_progress" && todo.active_form ? todo.active_form : todo.content,
  }));
}

// The header's text and the body for a call (TodosToolRenderContext):
//   main  "2/5", or "created 3 todos[, starting first]" (meta.is_new)
//   note  " · <task in progress>", " · completed N[, starting next]",
//         " · completed all" or " · starting task", else ""
//   ratio true when main is the done/total ratio
//   body  null, {type: "todos", items}, or {type: "started", text}
export function todosView(input, result) {
  const out = { main: "", note: "", ratio: false, body: null };
  const { value: params, ok } = decode(input ?? "", TODOS_PARAMS);
  if (!ok) return out;
  let done = 0;
  let current = "";
  for (const todo of params.todos) {
    if (todo.status === "completed") done++;
    if (todo.status === "in_progress") current = todo.active_form || todo.content;
  }
  out.main = `${done}/${params.todos.length}`;
  out.ratio = true;
  if (current) out.note = " · " + current;
  if (!result || !result.metadata) return out;
  const { value: meta, ok: metaOk } = decode(result.metadata, TODOS_META);
  if (!metaOk) return out;
  if (meta.is_new) {
    out.main = meta.just_started ? `created ${meta.total} todos, starting first` : `created ${meta.total} todos`;
    out.ratio = false;
    out.note = "";
    out.body = listBody(meta.todos);
    return out;
  }
  const completedNow = meta.just_completed.length > 0;
  const started = meta.just_started !== "";
  const all = meta.completed === meta.total;
  out.main = `${meta.completed}/${meta.total}`;
  out.ratio = true;
  if (completedNow && started) out.note = ` · completed ${meta.just_completed.length}, starting next`;
  else if (completedNow) out.note = all ? " · completed all" : ` · completed ${meta.just_completed.length}`;
  else if (started) out.note = " · starting task";
  else out.note = "";
  if (all) out.body = listBody(meta.todos);
  else if (started) out.body = { type: "started", icon: "→", text: meta.just_started };
  return out;
}

function listBody(todos) {
  const items = todoList(todos);
  return items.length ? { type: "todos", items } : null;
}

// The header text as one string, as the terminal shows it ("2/5 · task").
export function todosHeaderText(input, result) {
  const view = todosView(input, result);
  return view.main + view.note;
}

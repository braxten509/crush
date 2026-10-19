// CHAT-071: the To-Do tool's header and body (port of chat/todos.go:40-192).
import { describe, expect, it } from "vitest";
import { todoList, todosHeaderText, todosView } from "./todos.js";

const input = (todos) => JSON.stringify({ todos });
const result = (meta) => ({ content: "ok", metadata: JSON.stringify(meta), is_error: false });
const TODOS = [
  { content: "Write tests", status: "pending" },
  { content: "Read code", status: "completed" },
  { content: "Fix bug", status: "in_progress", active_form: "Fixing the bug" },
  { content: "Ship", status: "pending" },
];

describe("To-Do header", () => {
  it("shows done/total and the task in progress from the input", () => {
    expect(todosView(input(TODOS), null)).toMatchObject({ main: "1/4", note: " · Fixing the bug", ratio: true, body: null });
    expect(todosHeaderText(input([{ content: "Fix", status: "in_progress" }]), null)).toBe("0/1 · Fix");
    expect(todosHeaderText(input([]), null)).toBe("0/0");
  });
  it("shows nothing when the input does not decode", () => {
    expect(todosView("not json", result({ is_new: true, total: 3 }))).toEqual({ main: "", note: "", ratio: false, body: null });
  });
  it("says a new list was created, and lists it", () => {
    const view = todosView(input(TODOS), result({ is_new: true, todos: TODOS, just_started: "Fixing the bug", total: 4 }));
    expect(view.main).toBe("created 4 todos, starting first");
    expect(view.ratio).toBe(false);
    expect(view.note).toBe("");
    expect(view.body.items.map((i) => `${i.icon} ${i.text}`)).toEqual(["✓ Read code", "→ Fixing the bug", "• Write tests", "• Ship"]);
    expect(todosHeaderText(input(TODOS), result({ is_new: true, todos: TODOS, total: 4 }))).toBe("created 4 todos");
  });
  it("says what changed", () => {
    const meta = (fields) => result({ todos: TODOS, just_completed: [], just_started: "", completed: 2, total: 4, ...fields });
    expect(todosHeaderText(input(TODOS), meta({ just_completed: ["Read code"], just_started: "Fix bug" }))).toBe("2/4 · completed 1, starting next");
    expect(todosHeaderText(input(TODOS), meta({ just_completed: ["a", "b"] }))).toBe("2/4 · completed 2");
    expect(todosHeaderText(input(TODOS), meta({ just_completed: ["a"], completed: 4 }))).toBe("4/4 · completed all");
    expect(todosHeaderText(input(TODOS), meta({ just_started: "Fix bug" }))).toBe("2/4 · starting task");
    expect(todosHeaderText(input(TODOS), meta({}))).toBe("2/4");
  });
  it("shows the started task, or the whole list once all are done", () => {
    const started = todosView(input(TODOS), result({ todos: TODOS, just_started: "Fix bug", completed: 1, total: 4 }));
    expect(started.body).toEqual({ type: "started", icon: "→", text: "Fix bug" });
    const done = todosView(input(TODOS), result({ todos: TODOS.map((t) => ({ ...t, status: "completed" })), just_completed: ["Ship"], completed: 4, total: 4 }));
    expect(done.body.type).toBe("todos");
    expect(done.body.items).toHaveLength(4);
    expect(todosView(input(TODOS), result({ todos: [], completed: 0, total: 0 })).body).toBeNull();
  });
  it("keeps the input's header when the metadata does not decode", () => {
    expect(todosHeaderText(input(TODOS), { content: "ok", metadata: "{", is_error: false })).toBe("1/4 · Fixing the bug");
  });
});

describe("To-Do list", () => {
  it("puts done first, then in progress, then the rest, keeping their order", () => {
    const list = todoList([
      { content: "b", status: "pending" },
      { content: "a", status: "completed" },
      { content: "c", status: "cancelled" },
      { content: "d", status: "in_progress" },
      { content: "e", status: "completed" },
    ]);
    expect(list.map((i) => i.text)).toEqual(["a", "e", "d", "b", "c"]);
    expect(list.map((i) => i.icon)).toEqual(["✓", "✓", "→", "•", "•"]);
  });
});

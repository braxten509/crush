// The left list's todos (owner, 2026-11-09, pick C): each one a line of
// words, under a folder of the list or under "No project" (dir ""); no due
// dates. Ticking one takes it away. Kept in localStorage, which every window
// of the app shares; a change in one window reaches the others through the
// storage event (as lib/stores/listfolders.svelte.js does).
import { folderKey, keepFolders } from "./listfolders.svelte.js";

const KEY = "crush.todos";

export const listTodos = $state({ list: read() });

function read() {
  try {
    const list = JSON.parse(globalThis.localStorage?.getItem(KEY) ?? "[]");
    if (!Array.isArray(list)) return [];
    return list
      .filter((t) => t && typeof t.id === "string" && typeof t.text === "string" && t.text.trim() !== "")
      .map((t) => ({ id: t.id, text: t.text, dir: typeof t.dir === "string" ? t.dir : "" }));
  } catch {
    return [];
  }
}

function write(list) {
  listTodos.list = list;
  try {
    globalThis.localStorage?.setItem(KEY, JSON.stringify(list));
  } catch (error) {
    console.warn("Could not save the todos", error?.message ?? error);
  }
}

// A todo's folder stays a heading of the list while the todo is there.
function keepTheirFolders(list) {
  keepFolders(list.map((t) => t.dir).filter(Boolean));
}

// One folder's todos, oldest first ("" for No project).
export function todosIn(dir) {
  const k = folderKey(dir);
  return listTodos.list.filter((t) => t.dir === k);
}

export function addTodo(text, dir) {
  const words = text.trim();
  if (!words) return;
  const todo = { id: `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`, text: words, dir: folderKey(dir) };
  keepTheirFolders([todo]);
  write([...listTodos.list, todo]);
}

// Empty words take the todo away.
export function editTodo(id, text) {
  const words = text.trim();
  if (!words) return dropTodo(id);
  write(listTodos.list.map((t) => (t.id === id ? { ...t, text: words } : t)));
}

export function moveTodo(id, dir) {
  const k = folderKey(dir);
  const list = listTodos.list.map((t) => (t.id === id ? { ...t, dir: k } : t));
  keepTheirFolders(list.filter((t) => t.id === id));
  write(list);
}

// Ticked or deleted: either way it goes.
export function dropTodo(id) {
  write(listTodos.list.filter((t) => t.id !== id));
}

keepTheirFolders(listTodos.list);

globalThis.addEventListener?.("storage", (event) => {
  if (event.key === KEY) listTodos.list = read();
});

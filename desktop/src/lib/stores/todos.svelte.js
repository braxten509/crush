// The tray's to-do section (desktop-design 1.5.3; SIDE-031 to SIDE-034); the
// to-dos themselves are sessions.byId[sid].todos. A port of
// togglePillsExpanded and autoExpandPillsIfReasonable
// (internal/ui/model/pills.go:131-195).
import { hasIncomplete, progress as countTodos } from "../composer/trayrows.js";
import { sessions } from "./sessions.svelte.js";

export const todos = $state({ open: false, openedFor: {} }); // openedFor: sid → true once it opened by itself (SIDE-032)

// The desktop measure for the terminal's 40 rows (pillsHeightReasonableTerminalHeight).
export const AUTO_OPEN_MIN_HEIGHT = 900;

function shownTodos() {
  return sessions.byId[sessions.currentId]?.todos ?? [];
}

// SIDE-032: only while the shown chat has unfinished to-dos.
export function toggle() {
  if (!sessions.currentId || !hasIncomplete(shownTodos())) return false;
  todos.open = !todos.open;
  return true;
}

// { done, total, active: Todo | null }
export function progress(sid) {
  return countTodos(sessions.byId[sid]?.todos ?? []);
}

// Opens the list by itself once per chat, when the conversation column is at
// least 900 picture px tall and the chat has unfinished to-dos (SIDE-032).
export function autoOpen(columnHeight) {
  const sid = sessions.currentId;
  if (!sid || todos.open || todos.openedFor[sid]) return false;
  if (columnHeight < AUTO_OPEN_MIN_HEIGHT || !hasIncomplete(shownTodos())) return false;
  todos.open = true;
  todos.openedFor[sid] = true;
  return true;
}

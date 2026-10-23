// The list's rows in order (desktop-design-5-6 2.20; LIST-008, LIST-009,
// LIST-044): the open chats shared with the terminal as the list draws them
// (openlist.listedRows), then this window's terminal tabs in their list
// order. A row's number is its Alt+number, chats first
// (internal/ui/sessionhost/input.go:53-56); the strip shows it.
import { closeMain, mainView } from "../mainview.js";
import { conn, inFolder, switchFolder } from "../stores/connection.svelte.js";
import { folders } from "../stores/folders.svelte.js";
import { openChats, openInFolder } from "../stores/openchats.svelte.js";
import { parentOf } from "../stores/links.svelte.js";
import { busy, isHelperChat, openChat, sessions } from "../stores/sessions.svelte.js";
import { helperRows } from "../stores/helperchats.svelte.js";
import { rowState } from "../listmodel.js";
import { moveTab, showTerminal, terminals } from "../terminal/tabs.svelte.js";
import { folderGroups, listedRows, placeHelperRows } from "./openlist.js";
import { folderKey, listFolders } from "../stores/listfolders.svelte.js";
import { baseOf, clean, short } from "./picker.js";
import { leftList } from "./store.svelte.js";

// This window's terminal tabs in list order: TERM's own tab order, which the
// list's moves and drags change (moveTab), so a close picks the same next row.
export function orderedTerminals() {
  return terminals;
}

// A chat's second line: its folder's name, "~" for home (as the terminal's
// list writes it).
export function folderLabel(dir) {
  return dir ? baseOf(short(clean(dir), leftList.home)) : "";
}

// A temporary row while New chat saves its empty conversation.
export const DRAFT_ID = "new-chat";

function draftRow() {
  const dir = conn.project.path || conn.project.cwd || "";
  if (!sessions.draft || sessions.currentId || !dir) return null;
  return {
    kind: "chat",
    id: DRAFT_ID,
    title: "New chat",
    agent: "",
    time: "",
    at: 0,
    depth: 0,
    selected: true,
    closed: false,
    state: "",
    stateKind: "",
    branches: 0,
    expanded: false,
    folder: null,
    dir,
    draft: true,
  };
}

// The chats under their folders (owner, 2026-10-21): [{dir, label, rows}],
// the kept folders first (lib/stores/listfolders.svelte.js).
export function chatGroups(now = Date.now()) {
  const draft = draftRow();
  const rows = withHelpers(draft ? [draft, ...listedChats(now)] : listedChats(now));
  return folderGroups(rows, listFolders.dirs, folderKey).map((group) => ({
    ...group,
    label: short(group.dir, leftList.home),
  }));
}

// The chat rows in the list's drawn order, folder by folder.
export function chatRows(now = Date.now()) {
  return chatGroups(now).flatMap((group) => group.rows);
}

// The sub-agent chats this window shows (helperchats.svelte.js), each after
// its parent's row when the parent is listed, else at the end.
function withHelpers(rows) {
  const selectedId = shownTerminal() ? "" : sessions.currentId;
  return placeHelperRows(rows, helperRows({ here: inFolder, busy, rowState, selectedId }));
}

function listedChats(now) {
  // A sub-agent's chat is drawn by withHelpers, not as a branch.
  const byId = Object.fromEntries(Object.entries(sessions.byId).filter(([id]) => !isHelperChat(id)));
  return listedRows(openChats.list, {
    byId,
    parentOf,
    here: inFolder,
    // A shown terminal takes the band; no chat stays marked open beside it.
    selectedId: shownTerminal() ? "" : sessions.currentId,
    busy,
    now,
    timeOf: (s) => Math.max(s?.updated_at ?? 0, sessions.activity[s?.id] ?? 0),
    folderOf: folderLabel,
    catalog: folders.byId,
    states: folders.states,
  });
}

// The terminal tab the main column shows, else "".
export function shownTerminal() {
  const view = mainView.current;
  return view?.kind === "terminal" ? (view.props?.id ?? "") : "";
}

// Every row, numbered: {kind: "chat" | "terminal", id, title, number}; a chat
// of another folder also has folder: {dir}.
export function listRows() {
  const rows = chatRows().map((row) => ({ kind: "chat", id: row.id, title: row.title, folder: row.folder ?? null }));
  for (const tab of orderedTerminals()) rows.push({ kind: "terminal", id: tab.id, title: tab.title || "Terminal" });
  return rows.map((row, i) => ({ ...row, number: i + 1 }));
}

// The shown row: the terminal tab in the main column, else the open chat.
export function isShown(row) {
  const tab = shownTerminal();
  if (row.kind === "terminal") return row.id === tab;
  return !tab && (row.id === DRAFT_ID ? !sessions.currentId : row.id === sessions.currentId);
}

// Shows a row in this window: a chat in the conversation's place (a terminal
// tab over it steps aside, the chat already open stays as it is), a terminal
// tab in the main column (TERM's showTerminal).
export function showRow(row) {
  if (!row) return false;
  if (row.kind === "terminal") {
    showTerminal(row.id);
    // The window's folder follows the shown terminal's, unless the terminal
    // works inside it (owner, 2026-10-20).
    const cwd = terminals.find((t) => t.id === row.id)?.cwd ?? "";
    if (cwd && !within(cwd, conn.project.path || conn.project.cwd)) {
      switchFolder(cwd).catch((error) => console.error("folder switch failed", error));
    }
    return true;
  }
  if (mainView.current?.kind === "terminal") closeMain();
  if (row.id === DRAFT_ID) return true;
  // A chat of another folder opens in that folder, as the list's own rows do.
  if (row.folder) openInFolder(row.folder.dir, row.id).catch((error) => console.error("opening a chat failed", error));
  else if (row.id !== sessions.currentId) openChat(row.id);
  return true;
}

// Whether dir is folder or inside it.
export function within(dir, folder) {
  const clean = (path) => String(path ?? "").replace(/\/+$/u, "");
  const d = clean(dir);
  const f = clean(folder);
  return f !== "" && (d === f || d.startsWith(`${f}/`));
}

// Alt+1 … Alt+9.
export function showNumber(n) {
  return showRow(listRows()[n - 1]);
}

// Moves a terminal tab to place `to` among the terminal tabs (moveActive,
// input.go:262-286). Chats keep the list's day order, so only terminal tabs
// move (2.20, inferred).
export function moveTerminal(id, to) {
  return moveTab(id, to);
}

// Alt+Shift+↑ and Alt+Shift+↓: the shown row one place, within its group.
export function moveShown(by) {
  const id = shownTerminal();
  if (!id) return false;
  const order = orderedTerminals().map((tab) => tab.id);
  return moveTerminal(id, order.indexOf(id) + by);
}

// A terminal row's drag (input.go:149-163, :245-286; LIST-009, LIST-055):
// while the button is held after the press, the tab moves to the row under
// the pointer, within the terminal group. The pointer is captured by holder,
// the list part that holds the rows (it stays put while they reorder), so the
// drag ends at the release or cancel wherever the pointer is. rowElements()
// gives the terminal rows' elements in list order.
export function startDrag(event, id, holder, rowElements) {
  if (event.button !== 0 || !holder || event.target?.closest?.(".close")) return;
  const pointer = event.pointerId;
  try {
    holder.setPointerCapture(pointer);
  } catch {
    return;
  }
  leftList.dragging = id;
  const move = (e) => {
    if (e.pointerId !== pointer) return;
    if ((e.buttons & 1) === 0) {
      end();
      return;
    }
    let to = 0;
    rowElements().forEach((element, i) => {
      if (e.clientY >= element.getBoundingClientRect().top) to = i;
    });
    moveTerminal(id, to);
  };
  const end = () => {
    leftList.dragging = "";
    holder.removeEventListener("pointermove", move);
    holder.removeEventListener("pointerup", end);
    holder.removeEventListener("pointercancel", end);
    holder.removeEventListener("lostpointercapture", end);
    try {
      holder.releasePointerCapture(pointer);
    } catch {
      // Already released.
    }
  };
  holder.addEventListener("pointermove", move);
  holder.addEventListener("pointerup", end);
  holder.addEventListener("pointercancel", end);
  holder.addEventListener("lostpointercapture", end);
}

// The list's rows in order (desktop-design-5-6 2.20; LIST-008, LIST-009,
// LIST-044): the chat rows as the list draws them (listmodel.buildRows over
// ChatList's stores, without the search), then this window's terminal tabs in
// their list order. A row's number is its Alt+number, chats first
// (internal/ui/sessionhost/input.go:53-56); the strip shows it.
import { buildRows } from "../listmodel.js";
import { closeMain, mainView } from "../mainview.js";
import { isClosed } from "../stores/closed.svelte.js";
import { parentOf } from "../stores/links.svelte.js";
import { busy, openChat, sessions } from "../stores/sessions.svelte.js";
import { moveTab, showTerminal, terminals } from "../terminal/tabs.svelte.js";
import { leftList } from "./store.svelte.js";

// This window's terminal tabs in list order: TERM's own tab order, which the
// list's moves and drags change (moveTab), so a close picks the same next row.
export function orderedTerminals() {
  return terminals;
}

export function chatRows(now = Date.now()) {
  const list = sessions.order.map((id) => sessions.byId[id]);
  return buildRows(list, {
    parentOf,
    isClosed,
    selectedId: sessions.currentId,
    busy,
    now,
    timeOf: (s) => Math.max(s?.updated_at ?? 0, sessions.activity[s?.id] ?? 0),
  }).rows;
}

// The terminal tab the main column shows, else "".
export function shownTerminal() {
  const view = mainView.current;
  return view?.kind === "terminal" ? (view.props?.id ?? "") : "";
}

// Every row, numbered: {kind: "chat" | "terminal", id, title, number}.
export function listRows() {
  const rows = chatRows().map((row) => ({ kind: "chat", id: row.id, title: row.title }));
  for (const tab of orderedTerminals()) rows.push({ kind: "terminal", id: tab.id, title: tab.title || "Terminal" });
  return rows.map((row, i) => ({ ...row, number: i + 1 }));
}

// The shown row: the terminal tab in the main column, else the open chat.
export function isShown(row) {
  const tab = shownTerminal();
  return row.kind === "terminal" ? row.id === tab : !tab && row.id === sessions.currentId;
}

// Shows a row in this window: a chat in the conversation's place (a terminal
// tab over it steps aside, the chat already open stays as it is), a terminal
// tab in the main column (TERM's showTerminal).
export function showRow(row) {
  if (!row) return false;
  if (row.kind === "terminal") {
    showTerminal(row.id);
    return true;
  }
  if (mainView.current?.kind === "terminal") closeMain();
  if (row.id !== sessions.currentId) openChat(row.id);
  return true;
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

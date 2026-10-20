// The chat list's rows (desktop-design 3.3 "Nesting in the left list", 4.8,
// LIST-012). Pure functions: ChatList passes the stores in, so the rules are
// unit-tested without a window.
//
// Nesting is ready for phase 4: a branch hangs under its origin link's parent
// at any depth; a closed chat is left out and its children move up to the
// nearest open ancestor, or to the top (PLAN.md:103; the demo's
// visibleChildren, crush-gui-demos shared/core.js:67-76); a parent that is not
// in the list (deleted, engine 5.11) leaves its branches at the top. The open
// chat and its ancestors are expanded. Top-level chats come newest first, in
// the list's order (sessions.order); a chat's branches come oldest first, as
// the picture and the demo show them.

import { twelveHour } from "./timefmt.js";

export const DAY_LABELS = ["Today", "Yesterday", "Previous 7 days", "Previous 30 days", "Older"];
const WEEKDAYS = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];
const MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];
const DAY_MS = 86_400_000;

function startOfDay(ms) {
  const d = new Date(ms);
  return new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime();
}

// Whole local days from the day of `at` (Unix seconds) to the day of `now` (ms).
export function daysAgo(at, now) {
  return Math.round((startOfDay(now) - startOfDay(at * 1000)) / DAY_MS);
}

// The group a chat's last activity falls in.
export function dayLabel(at, now) {
  const days = daysAgo(at, now);
  if (days <= 0) return DAY_LABELS[0];
  if (days === 1) return DAY_LABELS[1];
  if (days <= 7) return DAY_LABELS[2];
  if (days <= 30) return DAY_LABELS[3];
  return DAY_LABELS[4];
}

// The row's time: the 12-hour clock today and yesterday, the weekday within a
// week, else the date (with the year when it is another year).
export function rowTime(at, now) {
  if (!at) return "";
  const date = new Date(at * 1000);
  const days = daysAgo(at, now);
  if (days <= 1) return twelveHour(date);
  if (days <= 7) return WEEKDAYS[date.getDay()];
  const text = `${MONTHS[date.getMonth()]} ${date.getDate()}`;
  return date.getFullYear() === new Date(now).getFullYear() ? text : `${text}, ${date.getFullYear()}`;
}

// LIST-012 (part): what the chat is doing, in the session host's order
// (internal/ui/model/session_host.go:58-66): a question or a permission prompt
// waiting for the user, then a run, then work left running in the background.
export function rowState(session, isBusy) {
  if (session?.pending_prompt === "permission") return { kind: "waiting", text: "Waiting for permission" };
  if (session?.pending_prompt === "question") return { kind: "waiting", text: "Waiting for your answer" };
  if (isBusy) return { kind: "working", text: "Working…" };
  if (session?.has_unfinished_work) return { kind: "background", text: "Background" };
  return null;
}

// Case-insensitive, anywhere in the title.
export function titleMatches(title, query) {
  const q = String(query ?? "").trim().toLocaleLowerCase();
  return q === "" || String(title ?? "").toLocaleLowerCase().includes(q);
}

export function emptyText(query, count) {
  if (count > 0) return "";
  const q = String(query ?? "").trim();
  return q ? `No chats match “${q}”.` : "No chats yet";
}

/**
 * The list as drawn. list: chat records in the list's order (newest first).
 * Options: parentOf(id) → parent id or null; isClosed(id); selectedId;
 * busy(id); expanded (ids whose branches show besides the open chat's line);
 * query; now (ms); timeOf(session) → Unix seconds of its last activity;
 * agentOf(session) → the agent's name or "".
 * Answers {items, rows, closedCount, empty}: items are the day headers
 * ({kind: "day", label}) and rows ({kind: "chat", ...row}) in drawing order;
 * rows are the chat rows alone.
 */
export function buildRows(list, options = {}) {
  const {
    parentOf = () => null,
    isClosed = () => false,
    selectedId = "",
    busy = () => false,
    expanded = [],
    query = "",
    now = Date.now(),
    timeOf = (s) => s?.updated_at ?? 0,
    agentOf = () => "",
  } = options;
  const chats = list.filter((s) => s && s.id);
  const byId = new Map(chats.map((s) => [s.id, s]));
  const closedCount = chats.filter((s) => isClosed(s.id)).length;

  // A parent outside the list (deleted, or never listed) makes a top-level chat.
  const parentIn = (id) => {
    const parent = parentOf(id);
    return parent && parent !== id && byId.has(parent) ? parent : null;
  };
  const children = new Map(); // parent id (null = the top) → chats
  for (const s of chats) {
    const parent = parentIn(s.id);
    if (!children.has(parent)) children.set(parent, []);
    children.get(parent).push(s);
  }
  for (const [parent, kids] of children) {
    if (parent !== null) kids.sort((a, b) => (a.created_at ?? 0) - (b.created_at ?? 0));
  }

  // visibleChildren: a closed chat gives its place to its own visible children.
  const visibleChildren = (parent, trail = new Set()) => {
    const out = [];
    for (const s of children.get(parent) ?? []) {
      if (trail.has(s.id)) continue;
      if (isClosed(s.id)) {
        trail.add(s.id);
        out.push(...visibleChildren(s.id, trail));
      } else out.push(s);
    }
    return out;
  };

  const open = new Set(expanded);
  for (let id = selectedId, hops = 0; id && hops < chats.length + 1; id = parentIn(id), hops += 1) open.add(id);

  const row = (s, depth, kids) => {
    const state = rowState(s, busy(s.id));
    const at = timeOf(s);
    return {
      kind: "chat",
      id: s.id,
      title: s.title || "New Session",
      agent: agentOf(s),
      time: rowTime(at, now),
      at,
      depth,
      selected: s.id === selectedId,
      closed: false,
      state: state?.text ?? "",
      stateKind: state?.kind ?? "",
      branches: kids,
      expanded: kids > 0 && open.has(s.id),
    };
  };

  const items = [];
  const rows = [];
  let lastLabel = null;
  const push = (r, slotAt) => {
    if (r.depth === 0) {
      const label = dayLabel(slotAt, now);
      if (label !== lastLabel) items.push({ kind: "day", label });
      lastLabel = label;
    }
    items.push(r);
    rows.push(r);
  };

  if (String(query ?? "").trim() !== "") {
    // Searching shows every open chat whose title matches, unnested.
    for (const s of chats) {
      if (isClosed(s.id) || !titleMatches(s.title, query)) continue;
      push(row(s, 0, 0), timeOf(s));
    }
    return { items, rows, closedCount, empty: emptyText(query, rows.length) };
  }

  // Every chat a walk from the top can reach, open or folded away; anything
  // else (a parent loop in bad data) is shown at the top rather than lost.
  const reachable = new Set();
  const reach = (parent) => {
    for (const s of visibleChildren(parent)) {
      if (reachable.has(s.id)) continue;
      reachable.add(s.id);
      reach(s.id);
    }
  };
  reach(null);

  const placed = new Set();
  const walk = (parent, depth, slotOf) => {
    for (const s of visibleChildren(parent)) {
      if (placed.has(s.id)) continue;
      placed.add(s.id);
      const kids = visibleChildren(s.id).filter((k) => !placed.has(k.id)).length;
      const r = row(s, depth, kids);
      push(r, depth === 0 ? slotOf(s) : 0);
      if (r.expanded) walk(s.id, depth + 1, slotOf);
    }
  };
  // A chat that moved up in a closed chat's place is grouped by that place.
  const topSlot = new Map();
  const slotOwner = (parent, owner) => {
    for (const s of children.get(parent) ?? []) {
      if (isClosed(s.id)) slotOwner(s.id, owner ?? s);
      else if (!topSlot.has(s.id)) topSlot.set(s.id, owner ?? s);
    }
  };
  slotOwner(null, null);
  walk(null, 0, (s) => timeOf(topSlot.get(s.id) ?? s));
  for (const s of chats) {
    if (!reachable.has(s.id) && !isClosed(s.id) && !placed.has(s.id)) {
      placed.add(s.id);
      push(row(s, 0, 0), timeOf(s));
    }
  }
  return { items, rows, closedCount, empty: emptyText(query, rows.length) };
}

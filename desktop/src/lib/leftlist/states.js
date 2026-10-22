// What each chat is doing (desktop-design-5-6 2.20; LIST-012's phase 6 part,
// LIST-018), in the session host's order (internal/ui/model/
// session_host.go:58-66; its marks, internal/ui/sessionhost/draw.go:274-297):
// phase 3's waiting, working and background (listmodel.rowState over the
// sessions store), then unread, ready, and unknown before the first read.
//
// is_busy, has_unfinished_work and pending_prompt come only from reads, never
// from events (engine design 5.1 a), so the list reads a chat (GET
// …/sessions/{sid}) when an event names it, at most once per 500 ms with one
// read in flight, and every 1 s while it works or runs in the background (the
// terminal's job poll, internal/ui/model/tasks.go:22).
import { SvelteSet } from "svelte/reactivity";
import { api } from "../engine/api.js";
import { rowState } from "../listmodel.js";
import { busy, readStart, rootOf, sessions, upsertSession } from "../stores/sessions.svelte.js";
import { unread } from "./unread.js";
import { conn, inFolder } from "../stores/connection.svelte.js";
import { openChats } from "../stores/openchats.svelte.js";

// Chats an event brought that no read has answered yet.
export const unknown = new SvelteSet();

// Phase 3's state of a chat: "waiting", "working", "background" or "".
export function phase3Kind(id) {
  return rowState(sessions.byId[id], busy(id))?.kind ?? "";
}

export function chatState(id) {
  const kind = phase3Kind(id);
  if (kind) return kind;
  if (unread.has(id)) return "unread";
  if (unknown.has(id)) return "unknown";
  return "ready";
}

// The state line LST adds to a chat row (lib/rowstates.js; ChatRow draws it
// on a row with none of phase 3's states): unread shows a check mark and
// "Done" (inferred wording); ready and unknown have no line.
export function rowStateOf(id) {
  return unread.has(id) ? { icon: "check", text: "Done" } : null;
}

// Each state's mark (look 5.2; the terminal's ›, !, spinner, muted spinner,
// ✓, ⠶ and ·): an Icon name. Strip.svelte colors them.
export const MARKS = {
  terminal: "terminal",
  waiting: "clock",
  working: "working",
  background: "working",
  unread: "check",
  ready: "circle",
  unknown: "dot",
};

const READ_GAP_MS = 500;
const reads = new Map(); // sid → {inFlight, again, last, timer}

function joinedFolder(id) {
  const listed = openChats.list.find((chat) => chat.id === id);
  return !listed?.dir || inFolder(listed.dir);
}

// The chat an event names, as a listed chat: a branch or helper's own chat
// counts for the chat it belongs to.
function listedChat(sid) {
  if (!sid) return "";
  return sessions.byId[sid] ? sid : rootOf(sid);
}

// Reads the chat soon: at once, or 500 ms after its last read began; a call
// while a read is on its way reads once more after it.
export function readSoon(sid) {
  const id = listedChat(sid);
  if (!id) return;
  if (!joinedFolder(id)) return;
  let entry = reads.get(id);
  if (!entry) {
    entry = { inFlight: false, again: false, last: 0, timer: null };
    reads.set(id, entry);
  }
  if (entry.inFlight) {
    entry.again = true;
    return;
  }
  if (entry.timer) return;
  entry.timer = setTimeout(() => readNow(id, entry), Math.max(0, entry.last + READ_GAP_MS - Date.now()));
}

async function readNow(id, entry) {
  entry.timer = null;
  if (!joinedFolder(id)) return;
  entry.inFlight = true;
  entry.last = Date.now();
  const since = readStart();
  const client = conn.clientId;
  try {
    const record = await api.getSession(id);
    if (conn.state === "connected" && conn.clientId === client && joinedFolder(id) && record?.id === id && sessions.byId[id]) {
      upsertSession(record, { since });
      unknown.delete(id);
    }
  } catch {
    // A chat that went away, or a lost engine: the next event or poll reads again.
  } finally {
    entry.inFlight = false;
    if (entry.again) {
      entry.again = false;
      readSoon(id);
    }
  }
}

// The 1 s poll: every listed chat that works or runs in the background.
export function pollWorking() {
  for (const id of sessions.order) {
    const kind = phase3Kind(id);
    if (kind === "working" || kind === "background") readSoon(id);
  }
}

// The chat an engine event names (1.4.2 payloads).
export function chatOfEvent(type, payload) {
  switch (type) {
    case "message":
    case "run_complete":
    case "helper":
      return payload?.session_id ?? "";
    case "session":
      return payload?.id ?? "";
    case "agent_event":
      return payload?.session_id || payload?.message?.session_id || "";
    default:
      return "";
  }
}

// A chat the list no longer has.
export function forgetChat(id) {
  unknown.delete(id);
  const entry = reads.get(id);
  if (entry?.timer) clearTimeout(entry.timer);
  reads.delete(id);
}

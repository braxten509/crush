// The all-folder catalog (GET saved-sessions), by chat id: the titles and
// times of open chats of other folders in the left list (openlist.js). Read
// when the window joins a folder and every 30 s while connected. With it,
// what those chats are doing, asked of their own folders' engines (states).
import { api } from "../engine/api.js";
import { folderChatStates } from "../engine/commands.js";
import { onEvent } from "../engine/events.js";
import { conn, inFolder } from "./connection.svelte.js";
import { openChats } from "./openchats.svelte.js";
import { sessions, setChatState } from "./sessions.svelte.js";

export const folders = $state({ byId: {}, states: {} });

const REFRESH_MS = 30_000;
let seq = 0;

export async function loadCatalog() {
  const mine = ++seq;
  try {
    const list = await api.savedSessions("");
    if (mine === seq) folders.byId = Object.fromEntries((Array.isArray(list) ? list : []).filter((s) => s?.ID).map((s) => [s.ID, s]));
  } catch (error) {
    console.error("chat catalog read failed", error?.message ?? error);
  }
}

// The open chats of other folders, what each is doing (owner, 2026-10-21:
// check every 30 s): {id: {is_busy, has_unfinished_work, pending_prompt}}.
// A chat this window holds a record of takes the state into that record.
export async function loadStates() {
  const chats = openChats.list.filter((c) => c?.id && c.dir && !inFolder(c.dir)).map((c) => ({ id: c.id, dir: c.dir }));
  const client = conn.clientId;
  let found = {};
  if (chats.length > 0) {
    try {
      found = (await folderChatStates(chats)) ?? {};
    } catch (error) {
      console.error("other folders' chat check failed", error?.message ?? error);
      return;
    }
  }
  if (conn.clientId !== client) return;
  const states = {};
  for (const c of chats) {
    const s = found[c.id];
    if (!s || inFolder(c.dir)) continue;
    states[c.id] = { is_busy: s.is_busy === true, has_unfinished_work: s.has_unfinished_work === true, pending_prompt: s.pending_prompt ?? "" };
    if (sessions.byId[c.id]) setChatState(c.id, states[c.id]);
  }
  folders.states = states;
}

let timer = null;

onEvent("connection", (_kind, ev) => {
  if (ev?.state !== "connected") return;
  loadCatalog();
  loadStates();
  timer ??= setInterval(() => {
    if (conn.state !== "connected" || globalThis.document?.visibilityState === "hidden") return;
    loadCatalog();
    loadStates();
  }, REFRESH_MS);
});

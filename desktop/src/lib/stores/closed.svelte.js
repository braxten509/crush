// Closed chats (desktop-design 3.3, 4.8; PLAN.md:103), kept per engine data
// folder in the desktop settings file (settings.closed_chats[engine
// data_dir], 1.2.8), so every desktop window agrees and the set survives a
// restart. This process's windows learn of a change from the
// `settings-changed` window event at once; other app processes through the
// settings watcher within half a second (B16). The ids are a SvelteSet: $state
// does not track a plain Set's add and delete.
//
// close and reopen change the set at once and keep the change while the
// write is on its way; a refused write puts it back with a notice.
// closeChat is the list's ×: when the shown chat closes, the window opens the
// first chat still listed, else the landing state (D15).
import { getCurrentWebviewWindow } from "@tauri-apps/api/webviewWindow";
import { SvelteSet } from "svelte/reactivity";
import { closed_set, settings_get } from "../engine/commands.js";
import { onEvent } from "../engine/events.js";
import { buildRows } from "../listmodel.js";
import { parentOf } from "./links.svelte.js";
import { openChat, sessions, showLanding } from "./sessions.svelte.js";
import { notify } from "./ui.svelte.js";

export const closed = $state({ ids: new SvelteSet() }); // from settings.closed_chats[engine data_dir]

const pending = new Map(); // session id → the closed value a write on its way sets
let lastSettings = null;
let projectKey = ""; // the engine's data folder (Ensured.data_dir) from the last connected event
let seq = 0; // bumped by every settings answer and event, so an older read never wins

const key = () => projectKey;

// The set for this project from a Settings answer, with the writes still on
// their way laid over it.
function apply(settings) {
  if (settings && typeof settings === "object") lastSettings = settings;
  const project = key();
  const want = new Set(project ? (lastSettings?.closed_chats?.[project] ?? []) : []);
  for (const [id, isClosed] of pending) {
    if (isClosed) want.add(id);
    else want.delete(id);
  }
  for (const id of [...closed.ids]) if (!want.has(id)) closed.ids.delete(id);
  for (const id of want) if (!closed.ids.has(id)) closed.ids.add(id);
}

async function readSettings() {
  const mine = ++seq;
  try {
    const settings = await settings_get();
    if (mine === seq) apply(settings);
  } catch (error) {
    console.error("settings read failed", error?.message ?? error);
  }
}

let listening = false;

function listen() {
  if (listening) return;
  listening = true;
  try {
    getCurrentWebviewWindow()
      .listen("settings-changed", (event) => {
        seq += 1;
        apply(event?.payload);
      })
      .catch(() => {});
  } catch {
    // Not inside the app window.
  }
}

// The window joined (or rejoined) its engine: the project's key may be new.
onEvent("connection", (_kind, ev) => {
  if (ev?.state !== "connected") return;
  const dataDir = ev.engine?.data_dir || "";
  if (dataDir) projectKey = dataDir;
  listen();
  apply(null);
  readSettings();
});

async function write(sessionId, isClosed) {
  const project = key();
  if (!sessionId) return false;
  if (!project) {
    notify("error", isClosed ? "Couldn't close this chat: not connected to Crush." : "Couldn't reopen this chat: not connected to Crush.");
    return false;
  }
  pending.set(sessionId, isClosed);
  apply(null);
  try {
    const settings = await closed_set(project, sessionId, isClosed);
    if (pending.get(sessionId) === isClosed) pending.delete(sessionId);
    seq += 1;
    apply(settings);
    return true;
  } catch (error) {
    if (pending.get(sessionId) === isClosed) pending.delete(sessionId);
    apply(null);
    const why = error?.message ?? String(error);
    notify("error", isClosed ? `Couldn't close this chat: ${why}` : `Couldn't reopen this chat: ${why}`);
    return false;
  }
}

// closed_set(project, id, true)
export async function close(sessionId) {
  return write(sessionId, true);
}

// closed_set(project, id, false)
export async function reopen(sessionId) {
  return write(sessionId, false);
}

export function isClosed(sessionId) {
  return closed.ids.has(sessionId);
}

// The chats the list shows, top to bottom, without the search.
export function listedChats() {
  return buildRows(
    sessions.order.map((id) => sessions.byId[id]),
    { parentOf, isClosed, selectedId: sessions.currentId },
  ).rows.map((row) => row.id);
}

// The list's ×: closes the chat; the shown chat makes way for the first chat
// still listed, or the landing state when none is left (D15), busy or not
// (newChat would refuse a busy chat; its reply carries on unseen).
export async function closeChat(sessionId) {
  if (!sessionId) return false;
  const wasShown = sessions.currentId === sessionId;
  const done = await close(sessionId);
  if (!done || !wasShown || sessions.currentId !== sessionId) return done;
  const next = listedChats().find((id) => id !== sessionId);
  if (next) await openChat(next);
  else showLanding();
  return true;
}

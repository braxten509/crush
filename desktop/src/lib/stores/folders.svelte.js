// The other folders' chats for the left list (lib/leftlist/folders.js): the
// all-folder catalog (GET saved-sessions), read when the window joins a
// folder and every 30 s while connected, so chats other windows start show
// too. Picking one of them switches the window to its folder in place
// (switchFolder), then opens it.
import { api } from "../engine/api.js";
import { onEvent } from "../engine/events.js";
import { conn, switchFolder } from "./connection.svelte.js";
import { openChat } from "./sessions.svelte.js";

export const folders = $state({ catalog: [] });

const REFRESH_MS = 30_000;
let seq = 0;

export async function loadCatalog() {
  const mine = ++seq;
  try {
    const list = await api.savedSessions("");
    if (mine === seq) folders.catalog = Array.isArray(list) ? list : [];
  } catch (error) {
    console.error("chat catalog read failed", error?.message ?? error);
  }
}

let timer = null;

onEvent("connection", (_kind, ev) => {
  if (ev?.state !== "connected") return;
  loadCatalog();
  timer ??= setInterval(() => {
    if (conn.state === "connected" && globalThis.document?.visibilityState !== "hidden") loadCatalog();
  }, REFRESH_MS);
});

// A chat of another folder: the window joins that folder, then shows it.
export async function openInFolder(dir, sessionId) {
  if (!(await switchFolder(dir))) return false;
  return openChat(sessionId);
}

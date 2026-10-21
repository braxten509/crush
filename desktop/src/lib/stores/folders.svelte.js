// The all-folder catalog (GET saved-sessions), by chat id: the titles and
// times of open chats of other folders in the left list (openlist.js). Read
// when the window joins a folder and every 30 s while connected.
import { api } from "../engine/api.js";
import { onEvent } from "../engine/events.js";
import { conn } from "./connection.svelte.js";

export const folders = $state({ byId: {} });

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

let timer = null;

onEvent("connection", (_kind, ev) => {
  if (ev?.state !== "connected") return;
  loadCatalog();
  timer ??= setInterval(() => {
    if (conn.state === "connected" && globalThis.document?.visibilityState !== "hidden") loadCatalog();
  }, REFRESH_MS);
});

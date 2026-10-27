// The close question (owner, 2026-10-27): when the last window closes while
// an engine this window keeps joined runs a chat's turn or a sub-agent
// (`crush spawn`), the "close" sheet asks first: Close window only (the
// engines keep the work going, as before), Stop engine (each engine with work
// stops it all and exits) or Cancel (nothing closes). Otherwise the window
// closes as it always did, with no question.
import { dialogRegistered, openDialog } from "./dialogs.js";
import { api } from "./engine/api.js";
import { windowCloseHold } from "./engine/commands.js";
import { conn, inFolder } from "./stores/connection.svelte.js";

export const CLOSE_SHEET = "close";
// How long one engine may take to tell its work; a slower one runs none.
const READ_LIMIT_MS = 3000;

// The running work of every folder this window keeps joined: {chats,
// helpers, dirs}, dirs being the folders whose engine runs some ("" the shown
// one). Folders of one project share an engine, counted once. An engine that
// does not answer, or predates the counts (GET /v1/engine busy_chats and
// running_helpers), or not within READ_LIMIT_MS, runs none.
export async function runningWork() {
  const dirs = ["", ...Object.keys(conn.folders ?? {}).filter((dir) => !inFolder(dir))];
  const infos = await Promise.all(dirs.map((dir) => limited(api.engine(dir))));
  const work = { chats: 0, helpers: 0, dirs: [] };
  const seen = new Set();
  infos.forEach((info, i) => {
    const chats = Number(info?.busy_chats) || 0;
    const helpers = Number(info?.running_helpers) || 0;
    const engine = `${info?.pid}:${info?.data_dir}`;
    if (chats + helpers === 0 || seen.has(engine)) return;
    seen.add(engine);
    work.chats += chats;
    work.helpers += helpers;
    work.dirs.push(dirs[i]);
  });
  return work;
}

// The promise's answer, or null when it fails or takes over READ_LIMIT_MS.
function limited(promise) {
  let timer;
  const late = new Promise((done) => (timer = setTimeout(() => done(null), READ_LIMIT_MS)));
  return Promise.race([promise.catch(() => null), late]).finally(() => clearTimeout(timer));
}

// The sheet's line: "2 chats and 1 sub-agent are still working."
export function workLine(chats, helpers) {
  const count = (n, word) => `${n} ${word}${n === 1 ? "" : "s"}`;
  const parts = [chats > 0 ? count(chats, "chat") : "", helpers > 0 ? count(helpers, "sub-agent") : ""].filter(Boolean);
  return `${parts.join(" and ")} ${chats + helpers === 1 ? "is" : "are"} still working.`;
}

// What closing this window does: {choice: "window" | "stop" | "cancel",
// stop: the folders whose engines stop}. The question waits as long as the
// user takes, so Rust's close fallback is held off first, before any engine
// is read (window_close_hold), and armed again once the window goes on
// closing. Holding also counts the other windows open, atomically: of two
// windows closing at once, the second sees the first gone and asks.
export async function askBeforeClose() {
  const plain = { choice: "window", stop: [] };
  if (!dialogRegistered(CLOSE_SHEET)) return plain;
  const others = await windowCloseHold(true).catch(() => 0);
  let answer = plain;
  try {
    if (others > 0) return plain;
    const work = await runningWork();
    if (work.chats + work.helpers === 0) return plain;
    const choice = await new Promise((done) => {
      if (!openDialog(CLOSE_SHEET, { chats: work.chats, helpers: work.helpers, answer: done })) done("window");
    });
    answer = { choice, stop: choice === "stop" ? work.dirs : [] };
    return answer;
  } finally {
    await windowCloseHold(false, answer.choice !== "cancel").catch(() => {});
  }
}

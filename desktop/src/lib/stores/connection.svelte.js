// The engine connection (desktop-design 1.5.3, 1.2.6, 2.9): the join, its
// state for the engine gate, the notices of SIDE-030 and 5.2, and the start-up
// reads that seed the other stores (START-007). Importing it loads every
// store, so all their event handlers are registered before the first batch.
import { untrack } from "svelte";
import { app_info, engine_open, engine_open_folder, engine_retry, engine_show_folder } from "../engine/commands.js";
import { onEvent } from "../engine/events.js";
import { probeSection } from "../probe.js";
import { accountPollConnected } from "./accountmeters.svelte.js";
import { loadLimits, startLimitsPoll, stopLimitsPoll } from "./limits.svelte.js";
import { listFolders } from "./listfolders.svelte.js";
import "./messages.svelte.js";
import "./prompts.svelte.js";
import { busy, loadSessions, openLaunchChat, sessions, setJoinWait, showLanding } from "./sessions.svelte.js";
import { notify } from "./ui.svelte.js";
import { startWorkspace } from "./workspace.svelte.js";

export const conn = $state({
  state: "idle", // "idle" | "connecting" | "connected" | "reconnecting" | "failed"
  attempt: 0,
  error: null, // AppError {code, message} of a failed join or reconnect
  workspace: null, // OpenResult.workspace, which has no env and no config (1.2.5)
  engine: null, // Ensured
  clientId: "",
  project: { cwd: "", path: "", dataDir: "", name: "" },
  // Every folder this window keeps joined, by cleaned path: one engine link
  // each, so a chat of any of them answers at once (owner, 2026-10-22).
  // { [dir]: { clientId, workspace, engine, name } }
  folders: {},
  notices: [], // [{kind, text, at}] the connection notices shown, newest last (the probe's list)
  version: null, // EngineVersion {version, protocol}
  freshEngine: false, // the last rejoin met a new engine (its workspace id changed)
  slow: false, // the join has taken over 400 ms: the gate shows "Starting Crush…" (2.9)
  launch: { session: "", continueLast: false }, // -s and -C (START-006)
  switching: 0, // folder switches under way: the shown chat may not be this folder's yet
});

// The helper keeps at most this many folders joined (connection.rs FOLDER_CAP);
// it drops the one shown longest ago when another joins.
export const FOLDER_CAP = 4;

const NOTICE_LIMIT = 20;
const STARTING_DELAY_MS = 400;

function plainError(error) {
  if (error && typeof error === "object" && typeof error.message === "string") {
    return { code: error.code ?? "", message: error.message };
  }
  return { code: "", message: String(error) };
}

function note(kind, text, ttlMs, action = null) {
  conn.notices = [...conn.notices, { kind, text, at: Date.now() }].slice(-NOTICE_LIMIT);
  notify(kind, text, ttlMs, action);
}

// Notices that come together show one after another, each for its time, so
// a newer one never hides the one before it unread.
function noteInTurn(list) {
  const [first, ...rest] = list;
  if (!first) return;
  note(first.kind, first.text, first.ttlMs);
  if (rest.length > 0) setTimeout(() => noteInTurn(rest), first.ttlMs);
}

function baseName(path) {
  const parts = String(path ?? "").split(/[\\/]/u).filter(Boolean);
  return parts[parts.length - 1] ?? "";
}

// A folder's path written one way, as Rust writes it.
function cleanDir(path) {
  const dir = String(path ?? "").trim();
  return dir === "/" ? dir : dir.replace(/\/+$/u, "");
}

// The answer of engine_open (1.2.6 OpenResult). The workspace is kept as Rust
// gave it: Rust already removed env and config (public_workspace, 1.2.5).
function applyOpen(opened) {
  conn.clientId = opened?.client_id ?? "";
  conn.workspace = opened?.workspace ?? null;
  conn.engine = opened?.engine ?? null;
  conn.version = opened?.version ?? null;
  conn.state = "connected";
  conn.attempt = 0;
  conn.error = null;
  const cwd = opened?.cwd || conn.project.cwd;
  const path = opened?.workspace?.path || cwd;
  conn.project = {
    cwd,
    path,
    dataDir: opened?.workspace?.data_dir || opened?.engine?.data_dir || "",
    name: baseName(path),
  };
  remember(opened, cwd);
  // The 5.2 notices (engine-design.md:545-550): the first window on a
  // project fixed its folder and its YOLO choice.
  const joined = [];
  if (opened?.workspace?.path && cwd && opened.workspace.path !== cwd) {
    joined.push({ kind: "info", text: `The AI works in ${opened.workspace.path}, the folder the first window on this project opened.`, ttlMs: 5000 });
  }
  const yolo = opened?.workspace?.yolo;
  if (typeof yolo === "boolean" && typeof opened?.sent_yolo === "boolean" && yolo !== opened.sent_yolo) {
    const text = yolo
      ? "Crush skips permission prompts on this project, as the first window on it chose."
      : "Crush asks before using tools on this project, as the first window on it chose.";
    joined.push({ kind: "info", text, ttlMs: 5000 });
  }
  noteInTurn(joined);
}

// One joined folder, as the page keeps it.
function remember(opened, dir) {
  const key = cleanDir(opened?.cwd || dir);
  if (!key) return "";
  conn.folders[key] = {
    clientId: opened?.client_id ?? "",
    workspace: opened?.workspace ?? null,
    engine: opened?.engine ?? null,
    name: baseName(opened?.workspace?.path || key),
  };
  return key;
}

// Whether this window keeps `dir` joined.
export function joinedFolder(dir) {
  const key = cleanDir(dir);
  return key !== "" && conn.folders[key] !== undefined;
}

function quietly(promise, what) {
  return Promise.resolve(promise).catch((error) => console.error(`${what} read failed`, error));
}

// The limits poll (SIDE-007) runs while connected and asks only while the
// shown chat is idle, as the terminal's isAgentBusy (internal/ui/model/
// send.go:23-31) is about the current session.
function pollLimits() {
  if (conn.state === "connected") startLimitsPoll({ idle: () => !busy(sessions.currentId) });
  else stopLimitsPoll();
  accountPollConnected(conn.state === "connected");
}

// START-007: the stores load in parallel and each may fail alone. The chat
// list comes first for -s and -C (START-006).
function seed() {
  return Promise.all([
    quietly(
      loadSessions().then(() => openLaunchChat(conn.launch)),
      "chat list",
    ),
    quietly(loadLimits(), "limits"),
    startWorkspace({ currentChat: () => sessions.currentId, connected: () => conn.state === "connected" }),
  ]);
}

let joinedOnce = false;
// The join under way, or the last one: from a folder switch's start (its
// leave included) to its end, so a chat picked meanwhile waits for it.
let joining = null;
setJoinWait(() => (conn.state === "connecting" || conn.switching > 0 ? joining : null));
// A folder switch left the old engine and its join failed: "Try again" joins
// again rather than asking Rust to reconnect a connection that is gone.
let mustJoin = false;

async function join() {
  conn.state = "connecting";
  conn.error = null;
  conn.slow = false;
  const timer = setTimeout(() => {
    if (conn.state === "connecting") conn.slow = true;
  }, STARTING_DELAY_MS);
  let opened;
  try {
    opened = await engine_open(conn.project.cwd);
  } catch (error) {
    conn.state = "failed";
    conn.error = plainError(error);
    return false;
  } finally {
    clearTimeout(timer);
    conn.slow = false;
  }
  applyOpen(opened);
  joinedOnce = true;
  mustJoin = false;
  pollLimits();
  joinListFolders();
  await seed();
  return true;
}

let started = null;

// app_info → engine_open(cwd) → seeds the other stores. App.svelte calls it once.
export function start() {
  started ??= (async () => {
    let info;
    try {
      info = await app_info();
    } catch (error) {
      conn.state = "failed";
      conn.error = plainError(error);
      return false;
    }
    conn.launch = { session: info?.session ?? "", continueLast: info?.continue_last === true };
    conn.project = { ...conn.project, cwd: info?.cwd ?? "" };
    joining = join();
    return joining;
  })();
  return started;
}

// "Try again": a join that never succeeded runs again; a connection lost
// after joining asks Rust to reconnect (engine_retry), whose progress comes
// as connection events.
export async function retry() {
  if (!joinedOnce || mustJoin) {
    if (conn.state === "connecting") return joining;
    if (!started) return start();
    joining = join();
    return joining;
  }
  conn.error = null;
  try {
    await engine_retry();
  } catch (error) {
    conn.state = "failed";
    conn.error = plainError(error);
  }
  return true;
}

function sameFolder(a, b) {
  const clean = (path) => String(path ?? "").replace(/\/+$/u, "");
  return clean(a) !== "" && clean(a) === clean(b);
}

// The window's folder is the shown chat's or terminal's (owner, 2026-10-20).
// Showing another folder no longer leaves this one: every folder the window
// keeps joined stays joined, and the one shown just becomes the one the page
// reads (owner, 2026-10-22: several folders open at once), so a switch costs
// no engine restart. The shown chat moves first (showLanding), unless
// keepShown: a kept chat of that folder already shows (openInFolder).
// Resolves true once the folder shows.
export async function switchFolder(dir, { keepShown = false } = {}) {
  conn.switching += 1;
  try {
    // A join or switch still under way ends first, so a chat of the folder
    // being shown, picked again meanwhile, switches once.
    for (let current = joining; current; current = joining) {
      await current.catch(() => {});
      if (joining === current) break;
    }
    if (!dir || sameFolder(dir, conn.project.cwd) || sameFolder(dir, conn.project.path)) return true;
    if (!keepShown) showLanding();
    conn.error = null;
    joining = showFolder(dir);
    return await joining;
  } finally {
    conn.switching -= 1;
  }
}

// Shows a folder: Rust joins it when this window does not keep it joined yet
// (dropping the folder shown longest ago when the cap is full), then makes it
// the one the page reads.
async function showFolder(dir) {
  let opened;
  try {
    opened = await engine_show_folder(dir);
  } catch (error) {
    console.error("could not show a folder", error?.message ?? error);
    conn.error = plainError(error);
    return false;
  }
  conn.launch = { session: "", continueLast: false };
  applyOpen(opened);
  return true;
}

export function inFolder(dir) {
  return sameFolder(dir, conn.project.path || conn.project.cwd);
}

// The folders the window keeps joined: the one it shows and, up to FOLDER_CAP
// in all, the rest of the left list, newest first (owner, 2026-10-22: every
// folder in the list joins and stays). A folder the window has no room for
// joins when it is shown.
let joins = Promise.resolve();
function joinListFolders() {
  joins = joins.then(joinMissingFolders).catch(() => {});
  return joins;
}

async function joinMissingFolders() {
  if (conn.state !== "connected") return;
  const here = conn.project.path || conn.project.cwd;
  const extras = Object.keys(conn.folders).filter((dir) => !sameFolder(dir, here)).length;
  const room = FOLDER_CAP - 1 - extras;
  const missing = listFolders.dirs.filter((dir) => cleanDir(dir) !== "" && !sameFolder(dir, here) && !joinedFolder(dir));
  if (room <= 0 || missing.length === 0) return;
  for (const dir of missing.reverse().slice(0, room)) {
    try {
      remember(await engine_open_folder(dir), dir);
    } catch (error) {
      console.error(`could not keep ${dir} joined`, error?.message ?? error);
    }
  }
}

// A folder a chat is opened in joins as the left list grows, so every folder
// the list keeps is live whether or not it has been shown.
$effect.root(() => {
  $effect(() => {
    const connected = conn.state === "connected";
    const dirs = listFolders.dirs.length; // read, so a new folder runs this
    if (!connected || dirs === 0) return;
    untrack(() => joinListFolders());
  });
});


// The failed notice (SIDE-030, internal/ui/model/agent_events.go:39-41).
const FAILED_TEXT = "Can't restore the connection to the Crush server. Restart Crush to recover.";

// A send refused while the connection has failed (engine_unavailable): Try
// again is the only way back (SIDE-030), so the failed notice stays, saying
// nothing was sent, instead of a notice without the button. True when it
// took the refusal; the caller shows its own notice otherwise.
export function refusedWhileFailed(error) {
  if (conn.state !== "failed" || !joinedOnce || error?.code !== "engine_unavailable") return false;
  notify("error", `${FAILED_TEXT} Nothing was sent.`, 60_000, { label: "Try again", run: retry });
  return true;
}

// The connection event (1.4.2; SIDE-030, internal/ui/model/agent_events.go:
// 25-65). A connected event with resync: true also runs every resync, which
// reloads the shown chat (events.js).
onEvent("connection", (_kind, ev) => {
  if (!ev?.state) return;
  const before = conn.state;
  conn.state = ev.state;
  conn.attempt = ev.attempt ?? 0;
  if (ev.workspace) conn.workspace = ev.workspace;
  if (ev.state === "failed") conn.error = ev.error ? plainError(ev.error) : conn.error;
  if (ev.state === "connected") {
    if (ev.engine) conn.engine = ev.engine; // the engine of this join (Ensured, 1.2.6)
    conn.error = null;
    conn.freshEngine = ev.fresh_engine === true;
    joinedOnce = true;
  }
  pollLimits();
  if (ev.state === "reconnecting") {
    note("warn", "Lost connection to the Crush server — reconnecting…", 30_000);
  } else if (ev.state === "failed" && joinedOnce) {
    note("error", FAILED_TEXT, 60_000, { label: "Try again", run: retry });
  } else if (ev.state === "connected" && (ev.resync === true || before === "reconnecting" || before === "failed") && before !== "connecting") {
    note("success", "Reconnected to the Crush server.", 5000);
  }
});

probeSection("connection", () => ({
  state: conn.state,
  attempt: conn.attempt,
  error: conn.error,
  workspace_id: conn.workspace?.id ?? "",
  client_id: conn.clientId,
  // Every folder this window keeps joined, the shown one included.
  folders: Object.keys(conn.folders),
  engine_version: conn.version?.version ?? conn.engine?.version ?? "",
  fresh_engine: conn.freshEngine,
  // The last connected event's engine: drivers read the socket for ground
  // truth and kill the pid (1.9).
  engine: { socket: conn.engine?.socket ?? "", pid: conn.engine?.pid ?? 0 },
  notices: conn.notices.map((n) => ({ kind: n.kind, text: n.text })),
}));

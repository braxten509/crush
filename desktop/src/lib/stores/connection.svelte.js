// The engine connection (desktop-design 1.5.3, 1.2.6, 2.9): the join, its
// state for the engine gate, the notices of SIDE-030 and 5.2, and the start-up
// reads that seed the other stores (START-007). Importing it loads every
// store, so all their event handlers are registered before the first batch.
import { app_info, engine_open, engine_retry, projectEnter } from "../engine/commands.js";
import { onEvent } from "../engine/events.js";
import { probeSection } from "../probe.js";
import { loadLimits, startLimitsPoll, stopLimitsPoll } from "./limits.svelte.js";
import "./messages.svelte.js";
import "./prompts.svelte.js";
import { busy, loadSessions, openLaunchChat, sessions, showLanding } from "./sessions.svelte.js";
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
  notices: [], // [{kind, text, at}] the connection notices shown, newest last (the probe's list)
  version: null, // EngineVersion {version, protocol}
  freshEngine: false, // the last rejoin met a new engine (its workspace id changed)
  slow: false, // the join has taken over 400 ms: the gate shows "Starting Crush…" (2.9)
  launch: { session: "", continueLast: false }, // -s and -C (START-006)
});

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

function quietly(promise, what) {
  return Promise.resolve(promise).catch((error) => console.error(`${what} read failed`, error));
}

// The limits poll (SIDE-007) runs while connected and asks only while the
// shown chat is idle, as the terminal's isAgentBusy (internal/ui/model/
// send.go:23-31) is about the current session.
function pollLimits() {
  if (conn.state === "connected") startLimitsPoll({ idle: () => !busy(sessions.currentId) });
  else stopLimitsPoll();
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
let joining = null;
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

// The window's folder is the shown chat's or terminal's (owner, 2026-10-20):
// picking one in another folder leaves this engine and joins that folder's in
// place, with no reload; the terminals and the list stay. The shown chat is
// left first (its turn runs on in the engine). Resolves true once joined.
export async function switchFolder(dir) {
  if (!dir || sameFolder(dir, conn.project.cwd) || sameFolder(dir, conn.project.path)) return true;
  if (joining) await joining.catch(() => {});
  showLanding();
  conn.state = "connecting";
  conn.error = null;
  try {
    await projectEnter(dir);
  } catch (error) {
    conn.state = "failed";
    conn.error = plainError(error);
    return false;
  }
  conn.project = { cwd: dir, path: dir, dataDir: "", name: baseName(dir) };
  conn.launch = { session: "", continueLast: false };
  mustJoin = true;
  joining = join();
  return joining;
}

export function inFolder(dir) {
  return sameFolder(dir, conn.project.path || conn.project.cwd);
}

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
  engine_version: conn.version?.version ?? conn.engine?.version ?? "",
  fresh_engine: conn.freshEngine,
  // The last connected event's engine: drivers read the socket for ground
  // truth and kill the pid (1.9).
  engine: { socket: conn.engine?.socket ?? "", pid: conn.engine?.pid ?? 0 },
  notices: conn.notices.map((n) => ({ kind: n.kind, text: n.text })),
}));

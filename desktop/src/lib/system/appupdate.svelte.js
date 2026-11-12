// Crush Desktop's own updates (owner, 2026-10-20: "an update button ... that
// automatically installs new versions ... and restarts it"; one button that
// changes state, at the foot of the right panel). The button checks the
// fork's main branch; when it has a newer build it offers Update, which
// builds and installs the app and the terminal crush (update.sh) and starts
// the app again with its chats still open. Each window checks quietly once
// when it first connects (a quick look at the branch's newest commit).
//
// Update asks first whether the engines restart too (owner, 2026-11-12:
// "when updating crush it should ask me if I want to restart the engine or
// not"; asked when Update is pressed, before the build). An engine keeps
// running the build it started on, so without a restart the update reaches
// only the app. Restart engines stops every engine this window keeps
// joined, with the work still running on it, once the build is installed;
// the app that starts next starts them again. Keep engines running is what
// an update did before. Cancel updates nothing.
import { getCurrentWebviewWindow } from "@tauri-apps/api/webviewWindow";
import { runningWork } from "../closeask.js";
import { dialogRegistered, openDialog } from "../dialogs.js";
import { appRestartHold, appUpdateCheck, appUpdateRun, enginesStop } from "../engine/commands.js";
import { onEvent } from "../engine/events.js";
import { conn } from "../stores/connection.svelte.js";
import { sessions } from "../stores/sessions.svelte.js";
import { notify } from "../stores/ui.svelte.js";

// phase: idle | checking | current (a moment after "Up to date") | ready |
// updating | restarting.
export const appUpdate = $state({ phase: "idle", current: "", latest: "" });

const CURRENT_MS = 3000;
let currentTimer = null;

export const short = (commit) => String(commit ?? "").slice(0, 7);

export async function checkApp({ quiet = false } = {}) {
  if (appUpdate.phase === "checking" || appUpdate.phase === "updating" || appUpdate.phase === "restarting") return;
  appUpdate.phase = "checking";
  let check;
  try {
    check = await appUpdateCheck();
  } catch (error) {
    appUpdate.phase = "idle";
    if (!quiet) notify("warn", `Couldn't check for updates: ${error?.message ?? error}`);
    return;
  }
  if (check?.error) {
    appUpdate.phase = "idle";
    if (!quiet) notify("warn", `Couldn't check for updates: ${check.error}`);
    return;
  }
  appUpdate.current = check?.current ?? "";
  appUpdate.latest = check?.latest ?? "";
  if (check?.newer) {
    appUpdate.phase = "ready";
    return;
  }
  appUpdate.phase = "current";
  clearTimeout(currentTimer);
  currentTimer = setTimeout(() => {
    if (appUpdate.phase === "current") appUpdate.phase = "idle";
  }, CURRENT_MS);
}

export const UPDATE_SHEET = "update-engines";
let asking = false;

// The update's question: "restart" | "keep" | "cancel". With no engine
// answering there is nothing to restart, and nothing is asked.
async function askEngines() {
  if (!dialogRegistered(UPDATE_SHEET)) return "keep";
  const work = await runningWork();
  if (work.dirs.length === 0) return "keep";
  return new Promise((done) => {
    if (!openDialog(UPDATE_SHEET, { chats: work.chats, helpers: work.helpers, waiting: work.waiting, answer: done })) done("keep");
  });
}

export async function updateApp() {
  if (asking || appUpdate.phase === "updating" || appUpdate.phase === "restarting") return;
  asking = true;
  let engines;
  try {
    engines = await askEngines();
  } finally {
    asking = false;
  }
  if (engines === "cancel") return;
  appUpdate.phase = "updating";
  notify("update", `Updating Crush Desktop and the terminal crush to ${short(appUpdate.latest)}: building…`, 3_600_000);
  let result;
  try {
    result = await appUpdateRun();
  } catch (error) {
    result = { ok: false, error: error?.message ?? String(error) };
  }
  if (!result?.ok) {
    appUpdate.phase = "ready";
    notify("error", `The update stopped: ${result?.error ?? "no answer"}.${result?.log ? ` Details: ${result.log}` : ""}`);
    return;
  }
  appUpdate.phase = "restarting";
  notify("success", `Updated to ${short(result.commit)}. Restarting…`, 10_000);
  try {
    await appRestartHold();
  } catch (error) {
    console.error("holding the open chats failed", error?.message ?? error);
  }
  if (engines === "restart") {
    // The engines as they are now: the build took a while.
    try {
      await enginesStop((await runningWork()).dirs);
    } catch (error) {
      console.error("stopping the engines failed", error?.message ?? error);
    }
  }
  // The app starts again on this window's folder and chat (owner,
  // 2026-10-21: it came back in the folder it was first started in).
  setTimeout(() => getCurrentWebviewWindow().relaunch({ cwd: conn.project.cwd || conn.project.path, session: sessions.currentId }), 800);
}

// The one button: Update once a newer build is known, else a check.
export function pressUpdate() {
  if (appUpdate.phase === "ready") return updateApp();
  return checkApp();
}

let checked = false;
onEvent("connection", (_kind, ev) => {
  if (ev?.state !== "connected" || checked) return;
  checked = true;
  checkApp({ quiet: true });
});

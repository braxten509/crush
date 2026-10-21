// Crush Desktop's own updates (owner, 2026-10-20: "an update button ... that
// automatically installs new versions ... and restarts it"; one button that
// changes state, at the foot of the right panel). The button checks the
// fork's desktop branch; when it has a newer build it offers Update, which
// builds and installs the app and the terminal crush (update.sh) and starts
// the app again with its chats still open. Each window checks quietly once
// when it first connects (a quick look at the branch's newest commit).
import { getCurrentWebviewWindow } from "@tauri-apps/api/webviewWindow";
import { appRestartHold, appUpdateCheck, appUpdateRun } from "../engine/commands.js";
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

export async function updateApp() {
  if (appUpdate.phase === "updating" || appUpdate.phase === "restarting") return;
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

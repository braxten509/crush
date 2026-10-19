// Starting and stopping the phone share (desktop-design-5-6 2.27; the
// terminal's internal/ui/model/remote.go:163-325). The engine runs the share;
// the window starts it (POST remote/start, with a launch ID once), shows its
// state, and stops it.
import { closeDialog, hasDialog, openDialog } from "../dialogs.js";
import { api } from "../engine/api.js";
import { setBadge } from "../stores/composer.svelte.js";
import { notify } from "../stores/ui.svelte.js";
import { share } from "./store.svelte.js";

let startSeq = 0;
let cancelled = false;

export const isOn = () => share.state === "on";

function takeStatus(status) {
  share.status = status ?? null;
  share.phones = Array.isArray(status?.phones) ? status.phones : [];
}

function turnedOn(status) {
  share.state = "on";
  takeStatus(status);
  setBadge("remote", "REMOTE");
  closeDialog("remote_setup");
}

// One start. `launchId` once per launch; `quiet` opens no sheet.
export async function startShare({ launchId = null, quiet = false } = {}) {
  const mine = ++startSeq;
  cancelled = false;
  if (share.state !== "on") share.state = "starting";
  share.quiet = quiet;
  share.error = "";
  let status;
  try {
    status = await api.startRemote(launchId ? { launch_id: launchId } : {});
  } catch (error) {
    if (mine !== startSeq) return;
    const text = error?.message ?? String(error);
    if (share.state !== "on") share.state = "off";
    share.error = text;
    if (quiet) notify("error", `Remote Control: ${text}`);
    else share.status = { state: "off", error: text };
    return;
  }
  if (mine !== startSeq) return;
  if (cancelled) {
    // A share that came up after Cancel is stopped (remote.go:255-258).
    if (status?.state === "on") api.stopRemote().catch(() => {});
    if (share.state !== "on") share.state = "off";
    return;
  }
  if (status?.state === "on") {
    const wasOn = share.state === "on";
    turnedOn(status);
    if (!quiet && !wasOn) openDialog("remote", {}, { grace: true });
    return;
  }
  if (status?.state === "setup") {
    share.state = "setup";
    takeStatus(status);
    if (quiet) notify("error", `Remote Control: ${status?.setup?.title || "setup is needed"}`);
    return;
  }
  share.state = "off";
  takeStatus(status);
  share.error = status?.error ?? "";
  if (quiet && share.error) notify("error", `Remote Control: ${share.error}`);
}

// The palette's "Remote Control" (remote.go:163-202).
export function openRemote() {
  if (share.state === "on") {
    openDialog("remote", {}, { grace: true });
    return;
  }
  if (hasDialog("remote_setup")) {
    openDialog("remote_setup");
    return;
  }
  if (share.state === "starting") return;
  share.status = null;
  openDialog("remote_setup", {}, { grace: true });
  startShare();
}

// Cancel in the setup sheet: drop a start in flight.
export function cancelStart() {
  cancelled = true;
  startSeq += 1;
  if (share.state !== "on") share.state = "off";
}

export async function stopShare({ silent = false } = {}) {
  startSeq += 1;
  try {
    await api.stopRemote();
  } catch (error) {
    if (!silent) notify("error", error?.message ?? String(error));
    return;
  }
  share.state = "off";
  share.phones = [];
  setBadge("remote", null);
  if (!silent) notify("info", "Remote Control is off");
}

// remote_status from the engine: phones come and go; the share may end.
export function onStatus(status) {
  if (!status) return;
  const before = new Set(share.phones);
  if (status.state === "on") {
    if (share.state !== "on") turnedOn(status);
    else takeStatus(status);
    for (const phone of share.phones) if (!before.has(phone)) notify("info", `Remote Control: ${phone} connected`);
  } else if (status.state === "off" && share.state === "on") {
    share.state = "off";
    takeStatus(status);
    setBadge("remote", null);
  }
}

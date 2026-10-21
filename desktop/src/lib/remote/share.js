// Starting and stopping phone shares (desktop-design-5-6 2.27; the
// terminal's internal/ui/model/remote.go and remote_engine.go). The engine
// runs one share per chat, whichever app turned it on (owner, 2026-10-21);
// this window starts the share of the chat it shows (POST remote/start,
// with a launch ID once), follows every share in the engine's remote_status
// events, and stops a chat's share however it was started.
import { closeDialog, hasDialog, openDialog } from "../dialogs.js";
import { api } from "../engine/api.js";
import { notify } from "../stores/ui.svelte.js";
import { share, shownChat, shownShare } from "./store.svelte.js";

let startSeq = 0; // bumped by every start, Cancel and stop: older answers are dropped
let latestStart = 0; // the newest start
let cancelledStart = 0; // the start that Cancel dropped
let hadShare = false; // the chat the newest start is for was shared already

// What the window heard of each chat's share: a list read before a change
// does not undo it.
let changes = 0;
let changed = {};

export const isShown = () => Boolean(shownShare());

// The share of `chat` is `status`, or none when it is null.
function heard(chat, status) {
  changes += 1;
  changed[chat] = changes;
  if (status) share.shares[chat] = status;
  else delete share.shares[chat];
}

// One start for the chat this window shows. `launchId` once per launch;
// `quiet` opens no sheet.
export async function startShare({ launchId = null, quiet = false } = {}) {
  const mine = ++startSeq;
  latestStart = mine;
  const chat = shownChat();
  hadShare = Boolean(share.shares[chat]);
  share.state = "starting";
  share.quiet = quiet;
  share.error = "";
  let status;
  try {
    status = await api.startRemote(launchId ? { session_id: chat, launch_id: launchId } : { session_id: chat });
  } catch (error) {
    if (mine !== startSeq) return;
    const text = error?.message ?? String(error);
    share.state = "off";
    share.error = text;
    if (quiet) notify("error", `Remote Control: ${text}`);
    else share.status = { state: "off", error: text };
    return;
  }
  // A share that came up after Cancel is stopped (remote.go:255-258),
  // unless a newer start wants it or the chat was shared already.
  if (mine === cancelledStart && latestStart === mine) {
    if (status?.state === "on" && !hadShare) api.stopRemote(status.session_id ?? chat).catch(() => {});
    share.state = "off";
    return;
  }
  if (mine !== startSeq) return;
  if (status?.state === "on") {
    share.state = "off";
    share.status = null;
    // It names the chat it shares: this window's, unless the window moved
    // on from its new chat meanwhile.
    heard(status.session_id ?? "", status);
    closeDialog("remote_setup");
    if (!quiet && shownShare()) openDialog("remote", {}, { grace: true });
    return;
  }
  if (status?.state === "setup") {
    share.state = "setup";
    share.status = status;
    if (quiet) notify("error", `Remote Control: ${status?.setup?.title || "setup is needed"}`);
    return;
  }
  share.state = "off";
  share.status = status ?? null;
  share.error = status?.error ?? "";
  if (quiet && share.error) notify("error", `Remote Control: ${share.error}`);
}

// The palette's "Remote control" (remote.go:163-202): the shown chat's
// share, started first when it is off.
export function openRemote() {
  if (shownShare()) {
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
  cancelledStart = latestStart;
  startSeq += 1;
  share.state = "off";
}

// Stops the share of `chat` (the shown one by default), whichever app
// started it.
export async function stopShare({ chat = shownChat(), silent = false } = {}) {
  startSeq += 1;
  try {
    await api.stopRemote(chat);
  } catch (error) {
    if (!silent) notify("error", error?.message ?? String(error));
    return;
  }
  heard(chat, null);
  if (!silent) notify("info", "Remote Control is off");
}

// Closing a chat stops its share (the list's ×, or the chat closed in the
// terminal). Nothing happens for a chat that has none.
export function chatClosed(chat) {
  if (chat && share.shares[chat]) stopShare({ chat, silent: true });
}

// remote_status from the engine: a chat's share starts or ends, from any
// app; phones come and go. A phone connecting to the shown chat's share is
// told.
export function onStatus(status) {
  if (!status) return;
  const chat = status.session_id ?? "";
  const before = share.shares[chat];
  if (status.state === "on") {
    heard(chat, status);
    if (!before || chat !== shownChat()) return;
    const seen = new Set(before.phones ?? []);
    for (const phone of status.phones ?? []) if (!seen.has(phone)) notify("info", `Remote Control: ${phone} connected`);
  } else if (status.state === "off") {
    heard(chat, null);
  }
}

// The engine's list of shares, read once joined: the events tell every
// change after that. A list of another folder's engine starts afresh.
let listedFor = null;
export async function loadShares(workspaceId = null) {
  if (workspaceId !== listedFor) {
    listedFor = workspaceId;
    share.shares = {};
    changed = {};
  }
  const since = changes;
  let list;
  try {
    list = await api.remoteShares();
  } catch (error) {
    console.error("reading the phone shares failed", error?.message ?? error);
    return;
  }
  const next = {};
  for (const status of Array.isArray(list) ? list : []) {
    const chat = status?.session_id ?? "";
    if ((changed[chat] ?? 0) <= since) next[chat] = status;
  }
  for (const [chat, status] of Object.entries(share.shares)) if ((changed[chat] ?? 0) > since) next[chat] = status;
  share.shares = next;
}

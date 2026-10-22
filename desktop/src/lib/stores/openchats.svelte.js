// The open chats shared with the terminal's session list (internal/openchats,
// the helper's openchats.rs; owner, 2026-10-20: "the two should be in sync").
// The left list draws them (lib/leftlist/openlist.js). A chat this window
// shows is listed (and its title kept current); the list's × takes a chat
// off for every app; a chat taken off elsewhere while this window shows it
// makes way, as the × does. The list is read every 1.5 s, and after each
// change made here. A deleted chat leaves the list.
import { untrack } from "svelte";
import { openChatsAdd, openChatsGet, openChatsRemove } from "../engine/commands.js";
import { setOtherFolder } from "../engine/api.js";
import { onEvent, onFolderResync } from "../engine/events.js";
import { chatClosed } from "../remote/share.js";
import { conn, inFolder, switchFolder } from "./connection.svelte.js";
import { keepFolders } from "./listfolders.svelte.js";
import { load, messages, rootChatOf } from "./messages.svelte.js";
import { isHelperChat, isKept, openChat, preloadChat, sessions, setOpenChats, showKept, showLanding, shownJoined } from "./sessions.svelte.js";

export const openChats = $state({ list: [], loaded: false });

const POLL_MS = 1500;
let seq = 0;
let seenShown = ""; // the shown chat, once a read has listed it

// Exported as refreshOpenChats for tests.
async function read() {
  const mine = ++seq;
  let list;
  try {
    list = await openChatsGet();
  } catch (error) {
    console.error("open chats read failed", error?.message ?? error);
    return;
  }
  if (mine !== seq) return;
  const before = openChats.list;
  openChats.list = Array.isArray(list) ? list : [];
  // A chat closed in the terminal stops its phone share (owner, 2026-10-21).
  for (const c of before) if (!openChats.list.some((n) => n.id === c.id)) chatClosed(c.id);
  openChats.loaded = true;
  // Their folders stay in the left list after their last chat closes.
  keepFolders(openChats.list.map((c) => c.dir));
  setOpenChats(openChats.list.map((c) => c.id));
  preloadOpen();
  const shown = sessions.currentId;
  const listed = openChats.list.some((c) => c.id === shown);
  if (shown && listed) seenShown = shown;
  // Closed in the terminal while shown here: it makes way.
  if (shown && !listed && seenShown === shown) {
    seenShown = "";
    leaveShown(shown);
  }
}

// Every open chat is loaded and kept behind the shown one, one at a time,
// this folder's first, so each shows at once when picked (owner,
// 2026-10-21) and stays drawn (TimelineStack; owner, 2026-10-22: "all chats
// should stay open"). Chats of other folders are read from their saved
// files (api.js setOtherFolder). Each is tried once per joined folder.
const tried = new Set();
let preloading = false;
async function preloadOpen() {
  if (preloading || conn.state !== "connected") return;
  preloading = true;
  try {
    for (;;) {
      const dir = folderNow();
      const untried = (c) => !tried.has(`${dir}\n${c.id}`) && c.id !== sessions.currentId;
      const next = openChats.list.find((c) => c.dir === dir && untried(c)) ?? openChats.list.find(untried);
      if (!next || conn.state !== "connected") break;
      tried.add(`${dir}\n${next.id}`);
      await preloadChat(next.id);
    }
  } finally {
    preloading = false;
  }
}

const sameDir = (a, b) => {
  const clean = (path) => String(path ?? "").replace(/\/+$/u, "");
  return clean(a) !== "" && clean(a) === clean(b);
};

// The folder of a listed chat (or of an agent's chat inside one) that this
// window has not joined, else "".
function otherFolderOf(sid) {
  // Until the window knows its own folder, every chat is read as before; so
  // is the chat it shows or opens, whose folder it joins.
  if (!folderNow()) return "";
  const root = rootChatOf(sid);
  if (!root || root === sessions.currentId || root === sessions.opening?.id) return "";
  const dir = root ? openChats.list.find((c) => c.id === root)?.dir : "";
  return dir && !inFolder(dir) ? dir : "";
}
setOtherFolder(otherFolderOf);

// The drawn chats of another folder are read again: that folder's events may
// have been missed (its stream just started, or this window just left it).
function rereadFolder(dir) {
  for (const c of openChats.list) {
    if (c.id === sessions.currentId || (dir && !sameDir(c.dir, dir)) || inFolder(c.dir)) continue;
    if (messages.bySession[c.id]?.status === "ready") load(c.id).catch((error) => console.error("rereading a chat failed", error?.message ?? error));
  }
}
onFolderResync(rereadFolder);

function folderNow() {
  return conn.project.path || conn.project.cwd || "";
}

let dropped = ""; // the shown chat just closed here: not listed again while it still shows
let lastWrite = ""; // the last add sent, so a write the file did not take is not sent again and again

// Lists the chat this window shows, or writes its new title.
async function listShown(id, title) {
  // A listed chat keeps its own folder: a chat of another folder shows
  // before the window has joined it (openInFolder), and was briefly listed
  // under the folder being left (owner, 2026-10-21).
  const known = openChats.list.find((c) => c.id === id);
  const dir = known?.dir || folderNow();
  // A sub-agent's chat is this window's only (helperchats.svelte.js).
  if (!id || !dir || id === dropped || isHelperChat(id)) return;
  if (known && known.dir === dir && (known.title === title || !title)) return;
  const write = JSON.stringify([id, dir, title]);
  if (known && write === lastWrite) return;
  lastWrite = write;
  try {
    await openChatsAdd(id, dir, title || "");
  } catch (error) {
    console.error("listing an open chat failed", error?.message ?? error);
  }
  read();
}

// Lists a chat of this folder again (a link to a closed branch, the search).
export async function listChat(id) {
  if (!id || !folderNow()) return false;
  await listShown(id, sessions.byId[id]?.title ?? "");
  return true;
}

// The shown chat makes way for the next listed chat of this folder, else the
// landing page.
function leaveShown(id) {
  const next = openChats.list.find((c) => c.id !== id && c.dir === folderNow() && sessions.byId[c.id]);
  if (next) openChat(next.id);
  else showLanding();
}

// The list's ×: off the list for every app, with its phone share; the shown
// chat makes way.
export async function closeListed(id) {
  if (!id) return false;
  const wasShown = sessions.currentId === id;
  openChats.list = openChats.list.filter((c) => c.id !== id);
  chatClosed(id);
  if (wasShown) {
    seenShown = "";
    dropped = id;
    leaveShown(id);
  }
  try {
    await openChatsRemove(id);
  } catch (error) {
    console.error("closing an open chat failed", error?.message ?? error);
  }
  read();
  return true;
}

// A chat of another folder: the window joins that folder, then shows it. A
// kept chat shows at once and the folder follows behind it (owner,
// 2026-10-20: "switching between two chats takes just as long").
export async function openInFolder(dir, sessionId) {
  if (!inFolder(dir) && isKept(sessionId)) {
    showKept(sessionId, { refresh: false });
    const joined = await switchFolder(dir, { keepShown: true });
    if (joined) shownJoined(sessionId);
    return joined;
  }
  if (!(await switchFolder(dir))) return false;
  return openChat(sessionId);
}

$effect.root(() => {
  // Whatever this window shows is open.
  $effect(() => {
    const id = sessions.currentId;
    const title = id ? (sessions.byId[id]?.title ?? "") : "";
    // An unlisted chat waits for the folder switch to end.
    const connected = conn.state === "connected" && conn.switching === 0;
    if (id !== dropped) dropped = "";
    if (id && connected) untrack(() => listShown(id, title));
  });
});

// A deleted chat is off the list for every app (the sessions store moves a
// shown one away itself).
onEvent("session", (kind, s) => {
  if (kind === "deleted") chatClosed(s?.id);
  if (kind !== "deleted" || !openChats.list.some((c) => c.id === s?.id)) return;
  if (seenShown === s.id) seenShown = "";
  openChats.list = openChats.list.filter((c) => c.id !== s.id);
  openChatsRemove(s.id)
    .catch((error) => console.error("closing a deleted chat failed", error?.message ?? error))
    .then(read);
});

let timer = null;

onEvent("connection", (_kind, ev) => {
  if (ev?.state !== "connected") return;
  read();
  // The folder just left is another folder now: its drawn chats catch up.
  rereadFolder("");
  timer ??= setInterval(() => {
    if (globalThis.document?.visibilityState !== "hidden") read();
  }, POLL_MS);
});

export { read as refreshOpenChats };

// The left list's own state (desktop-design-5-6 2.20 to 2.22; LST). The page
// shows the narrow strip instead of the list while leftList.strip: when the
// list is collapsed (the saved choice, list_collapsed in the desktop
// settings, so every window and the next start agree), or while the page is
// too narrow to hold it (forced; the saved choice is kept, as the terminal's
// listOpen, internal/ui/sessionhost/host.go:443-446).
import { runAction } from "../actions.js";
import { COLUMN_LIMITS } from "../columns.js";
import { hasDialog, openDialog } from "../dialogs.js";
import { settings_set } from "../engine/commands.js";
import { conn } from "../stores/connection.svelte.js";
import { notify } from "../stores/ui.svelte.js";
import { openTerminal } from "../terminal/tabs.svelte.js";

export const leftList = $state({
  collapsed: false, // the saved choice
  forced: false, // the page is narrower than the open list needs
  strip: false, // what the page shows: collapsed || forced
  failure: "", // the list's bottom line: "Couldn't start a session." or "Couldn't start the terminal."
  picker: null, // the folder picker while it is open (lib/leftlist/picker.js)
  home: "", // the home folder (app_info), written "~" in the picker
  dragging: "", // the terminal tab a drag is moving
  servers: [], // the listening ports of the project's chats, each once, in list order (servers.js)
  openFolds: {}, // fold → true: a program's row opened to show its servers
  picked: null, // null | {fold} | {port}: the picked server row
  serverRoom: Infinity, // how many server rows fit while leaving room for one chat (no longer measured: the servers are the right panel's)
});

// The open list needs the left column at its narrowest and the conversation
// at its narrowest: 740 picture px of the page's inner width (2.20).
export const LIST_ROOM = COLUMN_LIMITS.left.min + COLUMN_LIMITS.middleMin;

export function setCollapsed(on) {
  leftList.collapsed = on === true;
  leftList.strip = leftList.collapsed || leftList.forced;
}

export function setForced(on) {
  leftList.forced = on === true;
  leftList.strip = leftList.collapsed || leftList.forced;
}

// Alt+S, the list's collapse control and the strip's expand control
// (toggleList, host.go:459-466): the saved choice flips and is written to the
// desktop settings; a failed write is only logged and the choice holds for
// this window. A settings change that arrives while a write is on its way
// waits for it, so a quick second toggle never flickers back.
let writes = 0;
let waiting = null;

export function toggleList() {
  setCollapsed(!leftList.collapsed);
  save(leftList.collapsed);
  return true;
}

async function save(on) {
  writes += 1;
  try {
    waiting = (await settings_set("list_collapsed", on)) ?? waiting;
  } catch (error) {
    console.warn("Could not save the session list setting", error?.message ?? error);
  } finally {
    writes -= 1;
    if (writes === 0 && waiting) {
      const settings = waiting;
      waiting = null;
      applySettings(settings);
    }
  }
}

// The desktop settings (settings_get's answer or a settings-changed event).
export function applySettings(settings) {
  if (!settings || typeof settings !== "object") return;
  if (writes > 0) {
    waiting = settings;
    return;
  }
  setCollapsed(settings.list_collapsed === true);
}

// The right panel's folder name: the folder picker, to switch folders,
// unless a close box (TERM's terminal_close) is open (host.go:287-290).
export function openPicker(kind = "session") {
  if (hasDialog("terminal_close") || hasDialog("folder_picker")) return false;
  return openDialog("folder_picker", { mode: kind === "terminal" ? "terminal" : "session" });
}

// New chat and New terminal (the buttons, the strip's "+" and ">_", the
// Terminals group's "+", Alt+N, Alt+T) start in this window's folder with no
// picker: the list only shows this folder's chats and terminals (owner,
// 2026-10-20).
export function newChat() {
  if (hasDialog("terminal_close")) return false;
  runAction("chat.new");
  return true;
}

export function newTerminal() {
  if (hasDialog("terminal_close")) return false;
  startTerminal(conn.project.cwd || "");
  return true;
}

// A terminal tab in dir; a shell that can't start shows as a notice.
export async function startTerminal(dir) {
  try {
    const tab = await openTerminal({ cwd: dir });
    if (!tab?.id) throw new Error("no terminal tab");
    // The tab exists at once; whether its shell started comes later.
    const start = await tab.started;
    if (start?.error) throw new Error(start.error);
  } catch (error) {
    console.error("Could not start a terminal", error?.message ?? error);
    notify("error", "Couldn't start the terminal.");
  }
}

// Runs fn as an effect for the app's life (fn may return a cleanup), so the
// plain modules of the list can follow the stores.
export function watch(fn) {
  return $effect.root(() => {
    $effect(() => fn());
  });
}

let watchedPage = null;

// The page's inner width in picture pixels, as App.svelte measures it.
function innerWidth(page) {
  const scale = parseFloat(getComputedStyle(document.documentElement).fontSize) / 16 || 1;
  return (page.clientWidth - 4) / scale;
}

// Follows the page's width from the first list part that mounts (the open
// list's collapse control or the strip), which both sit inside the page.
export function watchWidth(element) {
  const page = element?.closest?.(".page");
  if (!page || page === watchedPage) return;
  watchedPage = page;
  const update = () => setForced(innerWidth(page) < LIST_ROOM);
  new ResizeObserver(update).observe(page);
  update();
}

// The window's own state (desktop-design 1.5.3): where focus is, the selected
// timeline item, the right-click menu, the picture viewer, the help sheet, the
// notice line, the composer slot and scroll requests. Sheets live in
// lib/dialogs.js's stack (1.10). Components read `ui`; only these functions
// change it.

export const ui = $state({
  focus: "composer", // "composer" | "timeline" | "panel" | "list" | "tray" | "slot" | "main" (the key scopes of 1.8; "main": the main column's helper or terminal view, desktop-design-5-6 7.1)
  selectedItem: null, // a timeline item key
  menu: null, // the open right-click menu (P3.5 decides its shape)
  viewer: null, // the picture in the viewer, e.g. {path}
  help: false,
  notice: null, // null | {id, kind, text, action}
  panelHidden: false,
  follow: true, // the timeline keeps its bottom in view (CHAT-099)
  slot: null, // null | SlotEntry (lib/types.js): what holds the typing box's place (Q8)
  scrollTo: null, // null | {sessionId, messageId, figureId?}: the timeline (P3.5) carries it out and clears it
});

let noticeCount = 0;
let noticeTimer = null;

// SIDE-028: kinds info, success, warn, error, update, plan, yolo. A newer
// notice replaces the older one; it clears itself after ttlMs (0 or less:
// it stays until replaced). action: null | {label, run()} for the text
// button at the line's right (4.14). Errors also go to the log.
// Errors stay until dismissed, replaced or fixed (owner, 2026-10-20: an error
// must be readable and offer its way out), whatever time the caller gave.
export function notify(kind, text, ttlMs = 5000, action = null) {
  const id = ++noticeCount;
  const sticky = kind === "error" || ttlMs <= 0;
  ui.notice = { id, kind, text, action, sticky };
  if (kind === "error") console.error(text);
  clearTimeout(noticeTimer);
  if (!sticky) {
    noticeTimer = setTimeout(() => {
      if (ui.notice?.id === id) ui.notice = null;
    }, ttlMs);
  }
  return id;
}

// Clears the notice line. Given a text, only while that text shows, so a
// sticky notice (ttlMs 0) is cleared without touching a newer one.
export function clearNotice(text) {
  if (text !== undefined && ui.notice?.text !== text) return false;
  clearTimeout(noticeTimer);
  ui.notice = null;
  return true;
}

export function openMenu(menu) {
  ui.menu = menu;
}

export function closeMenu() {
  ui.menu = null;
}

export function openViewer(image) {
  ui.viewer = image;
}

// Closes the menu, else the viewer, else the help sheet. True when one closed.
export function closeOverlay() {
  if (ui.menu) ui.menu = null;
  else if (ui.viewer) ui.viewer = null;
  else if (ui.help) ui.help = false;
  else return false;
  return true;
}

export function setHelp(open) {
  ui.help = open;
}

export function setFocus(target) {
  ui.focus = target;
}

export function selectItem(key) {
  ui.selectedItem = key;
}

export function setFollow(on) {
  ui.follow = on;
}

export function setPanelHidden(hidden) {
  ui.panelHidden = hidden;
}

// The right panel's own switch (owner, 2026-10-19): the desktop app keeps it
// apart from the terminal's compact mode, as the two are separate apps. It is
// kept in this window's local storage; shown unless turned off.
const PANEL_KEY = "crush-desktop.panel";
function readPanelOff() {
  try {
    return globalThis.localStorage?.getItem(PANEL_KEY) === "off";
  } catch {
    return false;
  }
}
export const panelSetting = $state({ off: readPanelOff() });

export function setPanelShown(shown) {
  panelSetting.off = !shown;
  try {
    globalThis.localStorage?.setItem(PANEL_KEY, shown ? "on" : "off");
  } catch {
    // Not kept past this run; the switch still works.
  }
}

// Puts a form in the typing box's place. Refuses (false) while another kind
// holds the slot, except that "permission", "question" and "secure" replace
// "plan", "customize" and "update"; "update" replaces nothing
// (desktop-design-5-6 7.1). The same kind replaces itself (a newer request).
const URGENT = new Set(["permission", "question", "secure"]);
const YIELDS = new Set(["plan", "customize", "update"]);
export function openSlot({ kind, component, props = {} }) {
  const holder = ui.slot?.kind;
  if (holder && holder !== kind) {
    if (!(URGENT.has(kind) && YIELDS.has(holder))) return false;
  }
  ui.slot = { kind, component, props };
  return true;
}

// Closes the slot only when that kind holds it.
export function closeSlot(kind) {
  if (ui.slot?.kind !== kind) return false;
  ui.slot = null;
  return true;
}

// Asks the timeline to show a message (and a figure's chip in it, 4.3).
export function revealInChat(target) {
  ui.scrollTo = target;
}

export function clearScrollTo() {
  ui.scrollTo = null;
}

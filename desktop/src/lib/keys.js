// The key router (desktop-design 1.8): one keydown listener on the window,
// in the capture phase, and bindings by scope. Pieces attach handlers with
// bindKey(scope, combo, handler); a handler takes the key by returning true
// (the key's default action and its way to the page are then stopped). The
// first scope that has the combo and whose handler returns true wins.
//
// Which scopes a key press asks, in order:
//   a right-click menu is open        → menu
//   the viewer or a sheet is open     → overlay
//   otherwise                         → overlay (the help sheet), then by ui.focus:
//                                       popup and composer, slot, tray, main, timeline, panel or list;
//                                       then global
// "main" is the middle column's helper or terminal view (lib/mainview.js,
// desktop-design-5-6 7.1). While that view is a terminal tab, no scope after
// main runs and a key no binding took is left to the page (the terminal
// widget), Ctrl+C included: it does not ask global first, so it reaches the
// shell like any other key.
// So no other global key works while the menu, the viewer or a sheet is open,
// as the terminal gives every key to its front dialog while one is open
// (internal/ui/model/keypress.go:111-114). The one exception is the quit key:
// Ctrl+C asks global before any other scope, and before a sheet's grace,
// unless the quit sheet is open, as the terminal opens its quit dialog over
// any dialog but itself (keypress.go:107-109).
// While the tray has focus, lib/tray.js's own rules run first (a section's
// onKey, then ← → ↑ Esc Tab). The zoom keys keep their own listener
// (lib/zoom.js) and are never routed here. A sheet opened with a grace
// swallows keys first (lib/grace.js).
//
// The combo "*" is any key: a scope's "*" handlers are tried after its exact
// combos (KIT's sheet keys, desktop-design-5-6 2.1).
import { graceAbsorbs, dialogs, hasDialog } from "./dialogs.js";
import { mainView } from "./mainview.js";
import { ui } from "./stores/ui.svelte.js";
import { trayKey } from "./tray.js";
import { ZOOM_DEFAULT, zoomForKey } from "./zoom.js";

export const SCOPES = ["menu", "overlay", "popup", "slot", "tray", "main", "composer", "timeline", "panel", "list", "global"];
const FOCUS_SCOPES = new Set(["slot", "tray", "main", "composer", "timeline", "panel", "list"]);
const QUIT_KEY = "Ctrl+C"; // the terminal's keyMap.Quit (internal/ui/model/keys.go:95-97)
const ANY = "*";

const bindings = new Map(SCOPES.map((scope) => [scope, new Map()])); // scope → combo → handlers

// combo is written like the table below: modifiers Ctrl, Alt, Shift, Meta, then
// the key's name ("Enter", "ArrowUp", "PageDown", "Escape", "Space", "F10").
// A printable key alone is the character itself, with no Shift ("k", "K",
// "?"); with Ctrl, Alt or Meta it is the letter in capitals and Shift is
// written ("Ctrl+K", "Ctrl+Shift+A"). Returns a function that unbinds it.
export function bindKey(scope, combo, handler) {
  const table = bindings.get(scope);
  if (!table) throw new Error(`bindKey: unknown scope "${scope}"`);
  const key = combo === ANY ? ANY : normalizeCombo(combo);
  if (!table.has(key)) table.set(key, []);
  table.get(key).push(handler);
  return () => {
    const list = table.get(key);
    const at = list ? list.indexOf(handler) : -1;
    if (at >= 0) list.splice(at, 1);
  };
}

// The scopes a key with this combo asks, in order.
export function activeScopes(combo = null) {
  const quit = isQuit(combo);
  const first = quit ? ["global"] : [];
  if (ui.menu) return [...first, "menu"];
  if (ui.viewer || dialogs.length > 0) return [...first, "overlay"];
  const scopes = [...first, "overlay"];
  if (ui.focus === "composer") scopes.push("popup");
  if (FOCUS_SCOPES.has(ui.focus)) scopes.push(ui.focus);
  if (!quit && !terminalFocused()) scopes.push("global");
  return scopes;
}

// A terminal tab has the focus: its keys belong to the shell.
function terminalFocused() {
  return ui.focus === "main" && mainView.current?.kind === "terminal";
}

function isQuit(combo) {
  return combo === QUIT_KEY && !hasDialog("quit") && !terminalFocused();
}

// The combo string for a keydown event, in bindKey's spelling.
export function comboOf(event) {
  let key = NAMES[event.key] ?? event.key;
  const command = event.ctrlKey || event.altKey || event.metaKey;
  // With Ctrl, Alt or Meta, a letter or digit is named by the key's place, so
  // the shortcuts work on any keyboard layout.
  if (command && key.length === 1 && !/^[\x21-\x7e]$/.test(key)) {
    const place = /^(?:Key([A-Z])|Digit([0-9]))$/.exec(event.code ?? "");
    if (place) key = place[1] ?? place[2];
  }
  return spell({ ctrl: event.ctrlKey, alt: event.altKey, shift: event.shiftKey, meta: event.metaKey }, key);
}

let started = false;

// Called once from main.js.
export function startKeys(win = window) {
  if (started) return;
  started = true;
  win.addEventListener("keydown", onKeyDown, { capture: true });
}

function onKeyDown(event) {
  if (event.defaultPrevented || event.isComposing) return;
  if (zoomForKey(event, ZOOM_DEFAULT) !== null) return;
  const combo = comboOf(event);
  if (!isQuit(combo) && graceAbsorbs(event)) return take(event);
  for (const scope of activeScopes(combo)) {
    if (scope === "tray" && trayKey(event)) return take(event);
    const table = bindings.get(scope);
    for (const handler of [...(table.get(combo) ?? []), ...(table.get(ANY) ?? [])]) {
      if (handler(event) === true) return take(event);
    }
  }
}

function take(event) {
  event.preventDefault();
  event.stopPropagation();
}

const NAMES = {
  " ": "Space",
  Spacebar: "Space",
  Esc: "Escape",
  Up: "ArrowUp",
  Down: "ArrowDown",
  Left: "ArrowLeft",
  Right: "ArrowRight",
  PgUp: "PageUp",
  PgDn: "PageDown",
  Del: "Delete",
  Apps: "ContextMenu",
  Menu: "ContextMenu",
};
const MODIFIER_NAMES = { ctrl: "ctrl", control: "ctrl", alt: "alt", option: "alt", shift: "shift", meta: "meta", cmd: "meta", super: "meta" };

function normalizeCombo(combo) {
  const plus = combo.length > 1 && combo.endsWith("++");
  const parts = plus ? [...combo.slice(0, -2).split("+"), "+"] : combo.split("+");
  const key = parts.pop();
  const mods = { ctrl: false, alt: false, shift: false, meta: false };
  for (const part of parts) {
    const name = MODIFIER_NAMES[part.toLowerCase()];
    if (!name) throw new Error(`bindKey: unknown modifier "${part}" in "${combo}"`);
    mods[name] = true;
  }
  let name = NAMES[key] ?? key;
  // "Shift+k" alone means the capital letter.
  if (mods.shift && name.length === 1 && !(mods.ctrl || mods.alt || mods.meta)) name = name.toUpperCase();
  return spell(mods, name);
}

function spell({ ctrl, alt, shift, meta }, key) {
  const printable = key.length === 1;
  const command = ctrl || alt || meta;
  const name = printable && command ? key.toUpperCase() : key;
  const mods = [];
  if (ctrl) mods.push("Ctrl");
  if (alt) mods.push("Alt");
  if (shift && (!printable || command)) mods.push("Shift");
  if (meta) mods.push("Meta");
  return [...mods, name].join("+");
}

// The shortcut map (1.8; the terminal's keys kept where sensible,
// internal/ui/model/keys.go:93-341). Each piece binds its own keys; this table
// is what the help sheet lists. phase: the phase whose action the key gets
// (reserved until then).
export const shortcuts = [
  { keys: ["Enter"], scope: "composer", action: "send", row: "TYPE-004" },
  { keys: ["Shift+Enter", "Ctrl+J"], scope: "composer", action: "new line", row: "TYPE-005" },
  { keys: ["Escape"], scope: "composer", action: "close popup or delete mode, leave history, else stop the run or the ! command", row: "TYPE-046" },
  { keys: ["Ctrl+Enter"], scope: "composer timeline", action: "interrupt and send queued", row: "TYPE-047" },
  { keys: ["ArrowUp", "ArrowDown"], scope: "composer", action: "history; Up recalls queued prompts", row: "TYPE-017" },
  { keys: ["Tab"], scope: "composer", action: "focus the timeline's last item", row: "TYPE-022" },
  { keys: ["Shift+Tab"], scope: "composer timeline", action: "mode cycle", row: "TYPE-023" },
  { keys: ["Ctrl+Y"], scope: "global", action: "YOLO toggle", row: "TYPE-024" },
  { keys: ["Ctrl+R"], scope: "composer", action: "attachment delete mode", row: "TYPE-038" },
  { keys: ["Ctrl+A", "Home", "Ctrl+E", "End"], scope: "composer", action: "line start / line end", row: "TYPE-009" },
  { keys: ["Ctrl+Shift+A", "Ctrl+Shift+C", "Ctrl+Shift+X"], scope: "composer", action: "select all, copy, cut", row: "TYPE-009" },
  { keys: ["Ctrl+V"], scope: "composer", action: "paste (text, image)", row: "TYPE-014" },
  { keys: ["Ctrl+Shift+V"], scope: "composer", action: "paste text from the clipboard", row: "TYPE-010" },
  { keys: ["Ctrl+G"], scope: "global", action: "full help", row: "SIDE-027" },
  { keys: ["Ctrl+T", "Ctrl+Space"], scope: "global", action: "show or hide to-dos", row: "SIDE-032" },
  { keys: ["Ctrl+D"], scope: "global", action: "session details (when the side panel is hidden)", row: "SIDE-003" },
  { keys: ["Ctrl+N"], scope: "global", action: "new chat", row: "LIST-033" },
  { keys: ["Ctrl+K"], scope: "global", action: "open the Sessions sheet", row: "PLAN.md:103" },
  { keys: ["Ctrl+End"], scope: "timeline composer", action: "jump to the bottom and follow", row: "CHAT-098" },
  { keys: ["ArrowUp", "ArrowDown", "k", "j"], scope: "timeline", action: "one line; selection follows", row: "CHAT-096" },
  { keys: ["Shift+ArrowUp", "Shift+ArrowDown", "K", "J"], scope: "timeline", action: "one item", row: "CHAT-096" },
  { keys: ["u", "d", "b", "f", "PageUp", "PageDown", "g", "G", "Home", "End"], scope: "timeline", action: "half page, page, top, bottom", row: "CHAT-096" },
  { keys: ["Space"], scope: "timeline", action: "expand or collapse the selected item", row: "CHAT-097" },
  { keys: ["c", "y", "C", "Y"], scope: "timeline", action: "copy the selected item", row: "CHAT-100" },
  { keys: ["Escape"], scope: "timeline", action: "clear the selection (busy: stop the run)", row: "CHAT-100" },
  { keys: ["Shift+ArrowLeft", "Shift+ArrowRight", "H", "L"], scope: "timeline", action: "side-scroll the selected shell or diff", row: "CHAT-043" },
  { keys: ["ArrowRight", "l", "ArrowLeft", "h"], scope: "timeline panel", action: "focus the side panel / back to the chat", row: "SIDE-012" },
  { keys: ["ArrowLeft", "ArrowRight"], scope: "tray", action: "switch section", row: "SIDE-033" },
  { keys: ["Ctrl+C"], scope: "global", action: "copy when text is selected; otherwise quit", row: "DLG-290s", phase: 5 },
  { keys: ["Ctrl+P", "Ctrl+L", "Ctrl+M", "Ctrl+S", "Ctrl+F"], scope: "global", action: "commands, models, sessions, add attachment", row: "—", phase: 5 },
  { keys: ["Ctrl+O", "Ctrl+X", "Ctrl+B", "Ctrl+Z"], scope: "global", action: "editor, helpers, background", row: "—", phase: 6 },
  { keys: ["Ctrl++", "Ctrl+-", "Ctrl+0"], scope: "global", action: "zoom", row: "PLAN.md:243" },
];

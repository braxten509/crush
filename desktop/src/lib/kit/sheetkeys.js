// The front sheet's keys (desktop-design-5-6 2.1, DLG-001, DLG-003). KIT's
// plugin binds sheetKey to every key of S3's overlay scope
// (bindKey("overlay", "*", sheetKey)), so it runs after the overlay's exact
// combos (the picture viewer's, the Changes sheet's) and never sees a key a
// sheet's grace swallowed (S3's router takes those first, lib/grace.js). Only
// the front sheet hears keys, as only the terminal's front dialog does
// (internal/ui/dialog/dialog.go:221-238). A key goes, in order, to:
//   1. the front sheet's own onKey: a sheet's keys win over the kit's
//      ("Screen keys win", CHECKLIST DLG-013);
//   2. the kit parts inside it, in page order: a list's ↑/↓ and Enter, a row
//      of buttons' ←/→, Enter, Space and shortcut letters;
//   3. Esc and Alt+Esc close the front sheet (dialog.go:13-16), any sheet;
//   4. Tab and Shift+Tab move the focus within a kit sheet, never out of it.
// Any other key goes on to the page, so the focused field types it.
//
// Each kit Sheet registers itself here by its id, and each kit part by its
// element; the probe's "kit" section reads both (plugins/kit.js). Nothing here
// is kept once a sheet closes.
import { closeFront, frontDialog } from "../dialogs.js";
import { comboOf } from "../keys.js";
import { ui } from "../stores/ui.svelte.js";

export const CLOSE_KEYS = ["Escape", "Alt+Escape"];

const sheets = new Map(); // sheet id → {onKey() → handler | null, root() → Element | null, report() → object}
const parts = new Map(); // element → {kind, keys?(event, combo) → true when taken, report?(), heavy?()}

// A kit Sheet while it is open. Returns the function that removes it.
export function setSheet(id, entry) {
  sheets.set(id, entry);
  return () => {
    if (sheets.get(id) === entry) sheets.delete(id);
  };
}

// A kit part (FilterList, Buttons, Field, Area, HelpFooter) while it is drawn.
export function setPart(element, entry) {
  parts.set(element, entry);
  return () => {
    if (parts.get(element) === entry) parts.delete(element);
  };
}

// The open kit sheets in no particular order: [{id, entry}].
export function openSheets() {
  return [...sheets].map(([id, entry]) => ({ id, entry }));
}

// The kit parts inside root, in page order: [{element, entry}].
export function partsIn(root) {
  if (!root) return [];
  const inside = [];
  for (const [element, entry] of parts) if (root.contains(element)) inside.push({ element, entry });
  return inside.sort((a, b) => (a.element.compareDocumentPosition(b.element) & Node.DOCUMENT_POSITION_FOLLOWING ? -1 : 1));
}

// The overlay's box for a sheet id (OverlayHost's [data-dialog]), kit or not.
export function dialogBox(id) {
  if (typeof document === "undefined") return null;
  for (const box of document.querySelectorAll("[data-dialog]")) if (box.dataset.dialog === id) return box;
  return null;
}

// KIT's handler for S3's overlay "*" binding: true when the key was taken.
export function sheetKey(event) {
  // The menu takes its own scope; the picture viewer sits above the sheets.
  if (ui.menu || ui.viewer) return false;
  const front = frontDialog();
  if (front === null) return false;
  const combo = comboOf(event);
  const sheet = sheets.get(front) ?? null;
  const onKey = sheet?.onKey() ?? null;
  if (typeof onKey === "function" && onKey(event, combo) === true) return true;
  const box = dialogBox(front);
  for (const { entry } of partsIn(box)) {
    if (entry.keys && entry.keys(event, combo) === true) return true;
  }
  if (CLOSE_KEYS.includes(combo)) {
    closeFront();
    return true;
  }
  if (sheet && (combo === "Tab" || combo === "Shift+Tab")) {
    moveFocus(sheet.root() ?? box, combo === "Shift+Tab");
    return true;
  }
  return false;
}

const FOCUSABLE = "button, input, textarea, select, a[href], [tabindex]:not([tabindex='-1'])";

// The places Tab reaches inside root, in page order.
export function focusables(root) {
  if (!root) return [];
  return [...root.querySelectorAll(FOCUSABLE)].filter(
    (el) => !el.disabled && !el.closest("[inert]") && el.getClientRects().length > 0 && getComputedStyle(el).visibility !== "hidden",
  );
}

// Tab: the next place inside root, round to the first; Shift+Tab backwards.
function moveFocus(root, back) {
  const list = focusables(root);
  if (list.length === 0) return;
  const at = list.indexOf(document.activeElement);
  const next = at < 0 ? (back ? list.length - 1 : 0) : (at + (back ? -1 : 1) + list.length) % list.length;
  list[next].focus();
}

// Whether the focus is in a place that types text, where letters, Space and
// ←/→ belong to the text.
export function typingIn(element = document.activeElement) {
  if (!element) return false;
  if (element.tagName === "TEXTAREA") return true;
  if (element.tagName === "INPUT") return !["button", "checkbox", "radio", "submit", "reset"].includes(element.type);
  return element.isContentEditable === true;
}

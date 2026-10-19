// What a terminal tab sends its program for the keys, wheel and pastes the
// widget does not handle the session host's way (desktop-design-5-6 2.25;
// internal/ui/sessionhost/input.go, terminal.go). Plain functions; the tab
// (tabs.svelte.js) and the plugin's `main` keys use them.

const ESC = "\x1b";

// Shift+Enter has no classic form; the session host sends Ctrl+J, Crush's
// other new-line key, which shells take as Enter (input.go:109-125).
export const SHIFT_ENTER = "\n";

// Shift+PgUp and Shift+PgDn on the alternate screen go to the program, as
// the emulator encodes them (CSI 5;2~ and CSI 6;2~; terminal.go:140-147).
export function shiftPageKey(down) {
  return `${ESC}[${down ? 6 : 5};2~`;
}

// The wheel's lines per notch, and on the alternate screen the arrows per
// notch (terminal.go:159-188).
export const WHEEL_LINES = 3;

// ↑ or ↓, count times, in the program's cursor-key mode (DECCKM), as the
// emulator's SendKey encodes them.
export function arrowKeys(up, applicationCursor, count = WHEEL_LINES) {
  return `${ESC}${applicationCursor ? "O" : "["}${up ? "A" : "B"}`.repeat(count);
}

// One wheel notch in pixels: WebKitGTK reports a notch as one line step of
// 40 pixels (WebCore's Scrollbar::pixelsPerLineStep), and a touchpad as many
// small pixel steps, which add up to notches here. Line and page deltas are
// scaled to pixels first.
export const NOTCH_PX = 40;

// Adds a wheel event to acc ({rest}) and returns the whole notches it made:
// negative upward. acc keeps the part of a notch that is left over.
export function wheelNotches(acc, { deltaY, deltaMode }) {
  const scale = deltaMode === 1 ? NOTCH_PX / WHEEL_LINES : deltaMode === 2 ? NOTCH_PX * WHEEL_LINES : 1;
  const total = (acc.rest ?? 0) + deltaY * scale;
  const notches = Math.trunc(total / NOTCH_PX);
  acc.rest = total - notches * NOTCH_PX;
  return notches;
}

// A paste as the program gets it: new lines as Enter (\r), as terminals send
// a paste, wrapped in the bracketed-paste marks when the program asked for
// them (the emulator's Paste, x/vt emulator.go:301-308).
export function pasteData(text, bracketed) {
  const body = String(text).replace(/\r?\n/g, "\r");
  return bracketed ? `${ESC}[200~${body}${ESC}[201~` : body;
}

// The widget's own focus reports; the tab sends its own instead, by whether
// it is shown and the window focused (LIST-023, LIST-052's rule). No key
// types either sequence alone.
export function isFocusReport(data) {
  return data === `${ESC}[I` || data === `${ESC}[O`;
}

export function focusReport(focused) {
  return focused ? `${ESC}[I` : `${ESC}[O`;
}

// OSC 7 (file://<host><path>): the folder, accepted only for an empty host,
// "localhost" or this computer's name, and an absolute path; else null
// (setWorkingDir, terminal.go:100-117).
export function osc7Folder(data, hostName = "") {
  let url;
  try {
    url = new URL(data);
  } catch {
    return null;
  }
  if (url.protocol !== "file:") return null;
  const host = url.hostname;
  if (host !== "" && host !== "localhost" && host !== hostName) return null;
  let path;
  try {
    path = decodeURIComponent(url.pathname);
  } catch {
    return null;
  }
  return path.startsWith("/") ? path : null;
}

// OSC 52 (<selection>;<base64>): the text a program copies, or null for a
// read ("?") or anything else (session.go:159-171; reads are refused).
export function osc52Text(data) {
  const parts = String(data).split(";");
  if (parts.length !== 2 || parts[1] === "?") return null;
  try {
    const bytes = Uint8Array.from(atob(parts[1]), (c) => c.charCodeAt(0));
    return new TextDecoder().decode(bytes);
  } catch {
    return null;
  }
}

// Text as UTF-8, and a binary string (the widget's onBinary) as its bytes.
export function utf8(text) {
  return new TextEncoder().encode(text);
}

export function binaryBytes(text) {
  return Uint8Array.from(text, (c) => c.charCodeAt(0) & 0xff);
}

// Bytes as base64 (pty_write) and back (pty_spawn's batches).
export function toBase64(bytes) {
  let out = "";
  for (let i = 0; i < bytes.length; i += 0x8000) out += String.fromCharCode(...bytes.subarray(i, i + 0x8000));
  return btoa(out);
}

export function fromBase64(text) {
  return Uint8Array.from(atob(text), (c) => c.charCodeAt(0));
}

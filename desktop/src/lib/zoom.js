// Zoom: the whole window scales in twelfths. 12/12 is the size of the approved
// picture; Compact (8/12) is the default. Ctrl + / Ctrl - / Ctrl 0 change it, and
// the choice is remembered. A ?zoom=1 address parameter (used by the capture
// that compares the build with the picture) forces a size for that page load.

export const ZOOM_MIN = 7;
export const ZOOM_MAX = 16;
export const ZOOM_DEFAULT = 8;
const KEY = "crush.zoom";

// Reads a zoom given as a scale ("1", "0.75") or in twelfths ("9/12").
export function parseZoom(text) {
  if (typeof text !== "string" || text.trim() === "") return null;
  const parts = text.split("/");
  let scale;
  if (parts.length === 2) scale = Number(parts[0]) / Number(parts[1]);
  else scale = Number(text);
  if (!Number.isFinite(scale) || scale <= 0) return null;
  return clampZoom(Math.round(scale * 12));
}

export function clampZoom(twelfths) {
  return Math.min(ZOOM_MAX, Math.max(ZOOM_MIN, Math.round(twelfths)));
}

// The zoom to start with: the address parameter, then the remembered choice, then Compact.
export function initialZoom(search, storage) {
  const fromAddress = parseZoom(new URLSearchParams(search).get("zoom") ?? "");
  if (fromAddress !== null) return { twelfths: fromAddress, remember: false };
  let saved = null;
  try {
    saved = parseZoom(storage?.getItem(KEY) ?? "");
  } catch {
    saved = null; // Storage can be unavailable; the default is fine then.
  }
  return { twelfths: saved ?? ZOOM_DEFAULT, remember: true };
}

export function applyZoom(twelfths, root) {
  root.style.setProperty("--zoom", String(twelfths / 12));
}

// The next zoom for a key press, or null when the key is not a zoom key.
export function zoomForKey(event, current) {
  if (!(event.ctrlKey || event.metaKey) || event.altKey) return null;
  switch (event.key) {
    case "+":
    case "=":
      return clampZoom(current + 1);
    case "-":
    case "_":
      return clampZoom(current - 1);
    case "0":
      return ZOOM_DEFAULT;
    default:
      return null;
  }
}

export function startZoom(win = window) {
  const root = win.document.documentElement;
  const start = initialZoom(win.location.search, win.localStorage);
  let current = start.twelfths;
  applyZoom(current, root);
  win.addEventListener("keydown", (event) => {
    const next = zoomForKey(event, current);
    if (next === null) return;
    event.preventDefault();
    current = next;
    applyZoom(current, root);
    if (start.remember) {
      try {
        win.localStorage.setItem(KEY, `${current}/12`);
      } catch {
        // Not remembered this time; the zoom still applies.
      }
    }
  });
  return () => current;
}

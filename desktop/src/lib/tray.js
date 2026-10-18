// The tray above the typing box (desktop-design 1.10, "The tray rules"; the
// Q7 tray). Sections register here; the Tray (P3.7) draws the visible ones in
// order. Phase 3: to-dos (order 10) and queue (20); phase 6 adds helpers (30)
// and background jobs (40). The tray shows only while a chat is open, nothing
// holds the slot and a section is visible.
import { SvelteMap } from "svelte/reactivity";
import { focusField } from "./stores/composer.svelte.js";
import { sessions } from "./stores/sessions.svelte.js";
import { setFocus, ui } from "./stores/ui.svelte.js";

const sections = new SvelteMap(); // id → section
const state = new SvelteMap([["selected", null]]);

// The visible sections in order, as a read-only array that Svelte templates
// and effects track (visible() may read any store).
export const traySections = liveList(() =>
  [...sections.values()].filter((section) => section.visible()).sort((a, b) => a.order - b.order),
);

// component draws the section's header line and its open body; header()
// gives the header's text to the probe and the screen reader; onKey(event)
// sees a key before the tray's own keys and takes it by returning true;
// downFocus: ↓ at the end of the field may enter the tray at this section.
export function registerTraySection({ id, order, visible, header, component, onKey = null, downFocus = false }) {
  sections.set(id, { id, order, visible, header, component, onKey, downFocus });
}

// True while the tray is on screen.
export function trayShown() {
  return Boolean(sessions.currentId) && !ui.slot && traySections.length > 0;
}

// Gives the tray focus: with {down: true} at the first visible section that
// has downFocus (the field calls this on ↓ and steps through its history only
// when it returns false), otherwise at the first visible section.
export function focusTray({ down = false } = {}) {
  if (!trayShown()) return false;
  const target = traySections.find((section) => !down || section.downFocus);
  if (!target) return false;
  state.set("selected", target.id);
  setFocus("tray");
  return true;
}

// The selected section's id, or null. When the selected section stops being
// visible, the selection moves to the next visible one (then the one before).
export function traySelected() {
  const id = state.get("selected");
  const visible = traySections;
  if (visible.length === 0) return null;
  if (visible.some((section) => section.id === id)) return id;
  const gone = sections.get(id);
  if (!gone) return visible[0].id;
  const next = visible.find((section) => section.order > gone.order) ?? visible[visible.length - 1];
  return next.id;
}

// Keys while the tray has focus (the "tray" scope of lib/keys.js). Returns
// true when the key was taken. The selected section's onKey comes first; then
// ← → move between sections, wrapping; ↑ and Esc go back to the field; Tab and
// Shift+Tab leave the tray (Tab to the field, Shift+Tab to the timeline); a key
// that types text goes back to the field and is typed there (not taken).
export function trayKey(event) {
  const selected = traySelected();
  if (selected === null) {
    focusField();
    return false;
  }
  const section = sections.get(selected);
  if (section.onKey && section.onKey(event) === true) return true;
  if (event.ctrlKey || event.altKey || event.metaKey) return false;
  switch (event.key) {
    case "ArrowLeft":
    case "ArrowRight": {
      const visible = traySections;
      const at = visible.findIndex((s) => s.id === selected);
      const step = event.key === "ArrowRight" ? 1 : -1;
      state.set("selected", visible[(at + step + visible.length) % visible.length].id);
      return true;
    }
    case "ArrowUp":
    case "Escape":
      focusField();
      return true;
    case "Tab":
      if (event.shiftKey) setFocus("timeline");
      else focusField();
      return true;
    default:
      if (event.key.length === 1) focusField();
      return false;
  }
}

function liveList(read) {
  return new Proxy([], {
    get(_, key) {
      const list = read();
      const value = Reflect.get(list, key);
      return typeof value === "function" ? value.bind(list) : value;
    },
    has: (_, key) => Reflect.has(read(), key),
    ownKeys: () => Reflect.ownKeys(read()),
    getOwnPropertyDescriptor: (_, key) => Reflect.getOwnPropertyDescriptor(read(), key),
    set: () => false,
    deleteProperty: () => false,
    defineProperty: () => false,
  });
}

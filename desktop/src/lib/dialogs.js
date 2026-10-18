// The window's one stack of sheets (desktop-design 1.10; a port of the
// terminal's dialog overlay, internal/ui/dialog/dialog.go). A sheet is
// registered once by id, then opened by id. OverlayHost draws the stack on the
// veil, front last, and makes every sheet but the front one inert. A sheet
// opened with {grace: true} absorbs keys for a moment (lib/grace.js); like the
// terminal, the grace belongs to the front sheet and ends when another sheet
// opens over it or it closes.
import { SvelteMap } from "svelte/reactivity";
import { createGrace, noteClosed } from "./grace.js";

const components = new Map(); // id → Component
const stack = new SvelteMap(); // id → {id, component, props}, in stack order (front last)
let frontGrace = null;

// A read-only array view of the stack: Svelte templates and effects that read
// it run again when the stack changes.
export const dialogs = liveList(() => [...stack.values()]);

export function registerDialog(id, Component) {
  components.set(id, Component);
}

// Pushes the sheet (true), or brings an open one to the front (false). An id
// nobody registered logs and returns false.
export function openDialog(id, props = {}, { grace = false } = {}) {
  const component = components.get(id);
  if (!component) {
    console.warn(`openDialog: no sheet is registered as "${id}"`);
    return false;
  }
  if (stack.has(id)) {
    bringToFront(id);
    return false;
  }
  stack.set(id, { id, component, props });
  frontGrace = grace ? createGrace(id) : null;
  return true;
}

export function bringToFront(id) {
  const entry = stack.get(id);
  if (!entry) return false;
  stack.delete(id);
  stack.set(id, entry);
  return true;
}

export function closeDialog(id) {
  if (!stack.has(id)) return false;
  if (frontDialog() === id) {
    noteClosed(id);
    frontGrace = null;
  }
  stack.delete(id);
  return true;
}

export function closeFront() {
  const id = frontDialog();
  return id === null ? false : closeDialog(id);
}

export function hasDialog(id) {
  return stack.has(id);
}

// The front sheet's id, or null.
export function frontDialog() {
  let front = null;
  for (const id of stack.keys()) front = id;
  return front;
}

// For lib/keys.js: true when the front sheet is still in its grace, so this
// key press must be swallowed.
export function graceAbsorbs(event) {
  return stack.size > 0 && frontGrace !== null && frontGrace.absorb(event);
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

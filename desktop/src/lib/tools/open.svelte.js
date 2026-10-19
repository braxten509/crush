// Open states of tool rows for the window's life (CHAT-080: a group keeps
// its open state by group id after new events), and the items the timeline
// keys act on. The timeline (P3.5) may call toggleOpen(key) for Space; the
// keys bound in lib/plugins/tools.js act on ui.selectedItem through the
// mounted items registered here.
import { SvelteSet } from "svelte/reactivity";

// Open groups, tools and shell outputs, by item key or tool call id.
export const opened = new SvelteSet();
// Groups whose earlier tools ("… N earlier") show.
export const allShown = new SvelteSet();

export const isOpen = (id) => opened.has(id);

export function setOpen(id, open) {
  if (open) opened.add(id);
  else opened.delete(id);
}

export function toggle(id) {
  setOpen(id, !opened.has(id));
  return opened.has(id);
}

export function showAll(groupId) {
  allShown.add(groupId);
}

// Mounted items: key → {toggle(), copy(), scroll(dir)} (scroll for shell
// output: dir −1 left, +1 right).
const mounted = new Map();

export function registerItem(key, actions) {
  mounted.set(key, actions);
  return () => {
    if (mounted.get(key) === actions) mounted.delete(key);
  };
}

export const itemActions = (key) => mounted.get(key) ?? null;

// Space on a selected tool item: true when the item is one of ours.
export function toggleOpen(key) {
  const actions = mounted.get(key);
  if (!actions?.toggle) return false;
  actions.toggle();
  return true;
}

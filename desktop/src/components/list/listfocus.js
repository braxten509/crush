// The chat list's search field, for the keys and start-up code that focus it
// (Ctrl+K, PLAN.md:103; START-021's errors). ChatList registers the field
// when it mounts.
import { setFocus } from "../../lib/stores/ui.svelte.js";

let field = null;

export function setSearchField(element) {
  field = element;
}

// True when the field took focus.
export function focusSearch() {
  if (!field || !field.isConnected) return false;
  field.focus();
  field.select?.();
  setFocus("list");
  return document.activeElement === field;
}

export function searchFocused() {
  return Boolean(field) && document.activeElement === field;
}

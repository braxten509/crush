// The middle column's other views (desktop-design-5-6 6.4; scaffold S5,
// complete): a helper's chat (LIVE) or a terminal tab (TERM) takes the
// conversation's place, which MainHost draws while Conversation stays mounted
// underneath, so its draft and scroll survive. One view at a time: a new one
// replaces the old. Showing a view gives it the focus ("main", the key scope
// of the same name); closing gives the focus back to the typing box.
import { SvelteMap } from "svelte/reactivity";
import { focusField } from "./stores/composer.svelte.js";
import { setFocus } from "./stores/ui.svelte.js";

const state = new SvelteMap([["current", null]]);

// mainView.current: null | MainViewEntry {kind: "helper" | "terminal", component, props} (lib/types.js).
export const mainView = {
  get current() {
    return state.get("current");
  },
};

// Shows the view; close() takes it away again, unless another view replaced it.
// key: a view whose parts are fixed when it mounts (a helper's chat) gives
// one, and a different key mounts it anew instead of reusing it.
export function showMain({ kind, component, props = {}, key = null }) {
  const entry = { kind, component, props, key };
  state.set("current", entry);
  setFocus("main");
  return {
    close() {
      if (state.get("current") !== entry) return false;
      closeMain();
      return true;
    },
  };
}

export function closeMain() {
  if (state.get("current") === null) return false;
  state.set("current", null);
  focusField();
  return true;
}

// The window's share (desktop-design-5-6 2.27 "State"): its state, the last
// RemoteStatus, the phones it has seen.
import { composer } from "../stores/composer.svelte.js";
import { sessions } from "../stores/sessions.svelte.js";
import { resetPresence, sendPresence } from "./presence.js";
export const share = $state({
  state: "off", // "off" | "starting" | "setup" | "on"
  status: null, // the last proto.RemoteStatus
  phones: [],
  quiet: false, // the current start came from a launch ID: no sheet
  error: "",
});

// Presence follows the shown chat, the window's focus and plan mode while
// the share is on (REM-011).

const focus = $state({ window: typeof document === "undefined" ? false : document.hasFocus() });
let watching = false;

export function watchPresence() {
  if (watching) return;
  watching = true;
  window.addEventListener("focus", () => (focus.window = true));
  window.addEventListener("blur", () => (focus.window = false));
  $effect.root(() => {
    $effect(() => {
      if (share.state !== "on") {
        resetPresence();
        return;
      }
      sendPresence({ sessionId: sessions.currentId, focused: focus.window, plan: composer.input === "plan" });
    });
  });
}

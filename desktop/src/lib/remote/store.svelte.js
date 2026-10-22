// The phone shares this window sees (desktop-design-5-6 2.27; owner,
// 2026-10-21: "One remote per chat - the chat GUI should be equivalent oon
// the phone and doesn't matter if the terminal or gui started it"): the
// engine runs one share per chat, whichever app turned it on, several at
// once. `shares` holds them by chat, "" for this window's new chat; `state`
// is a start in flight.
import { composer } from "../stores/composer.svelte.js";
import { conn } from "../stores/connection.svelte.js";
import { sessions } from "../stores/sessions.svelte.js";
import { resetPresence, sendPresence } from "./presence.js";
export const share = $state({
  shares: {}, // chat → its last proto.RemoteStatus, while on
  state: "off", // the start in flight: "off" | "starting" | "setup"
  status: null, // the start's last proto.RemoteStatus: its setup step or error
  quiet: false, // the current start came from a launch ID: no sheet
  error: "",
});

// The chat this window shows; "" for its new chat.
export const shownChat = () => sessions.currentId ?? "";

// The share of the chat this window shows, or null.
export const shownShare = () => share.shares[shownChat()] ?? null;

// What this window shows goes to the engine whenever it changes, sharing or
// not: a share's phone hears whether a window showing its chat is focused,
// and the window's new-chat share moves to the chat it shows (REM-011).

const focus = $state({ window: typeof document === "undefined" ? false : document.hasFocus() });
let watching = false;

export function watchPresence() {
  if (watching) return;
  watching = true;
  window.addEventListener("focus", () => (focus.window = true));
  window.addEventListener("blur", () => (focus.window = false));
  $effect.root(() => {
    $effect(() => {
      if (conn.state !== "connected") {
        resetPresence();
        return;
      }
      sendPresence({ sessionId: sessions.currentId, focused: focus.window, plan: composer.input === "plan" });
    });
  });
}

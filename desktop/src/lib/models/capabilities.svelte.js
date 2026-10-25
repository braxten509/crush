// What the shown chat's own model can do (desktop-design-5-6 2.3, 7.4; GET
// agent/capabilities?session_id=…): its CLI kind, whether it can move work to
// the background (LIVE), and G-8's three toggle supports (the palette's FAST,
// Ultracode and Thinking entries). Every chat owns its model (owner,
// 2026-10-24), so the answer is for one chat, or for the last pick with no
// chat open. Read when the shown chat or its model changes (live/helpers
// startWatches), on every resync and after config_changed; a late answer for
// a chat no longer shown is dropped.
import { api } from "../engine/api.js";
import { onEvent, onResync } from "../engine/events.js";
import { sessions } from "../stores/sessions.svelte.js";

// An AgentCapabilities (lib/engine/wire.js), and the chat it is for.
export const capabilities = $state({ sessionId: "", kind: "", can_background: false, supports_fast: false, supports_ultracode: false, supports_thinking_toggle: false });

let reading = 0;
export async function refreshCapabilities(sid = sessions.currentId ?? "") {
  const seq = ++reading;
  try {
    const read = await api.capabilities(sid);
    if (seq !== reading || sid !== (sessions.currentId ?? "") || !read || typeof read !== "object") return;
    capabilities.sessionId = sid;
    capabilities.kind = typeof read.kind === "string" ? read.kind : "";
    capabilities.can_background = read.can_background === true;
    capabilities.supports_fast = read.supports_fast === true;
    capabilities.supports_ultracode = read.supports_ultracode === true;
    capabilities.supports_thinking_toggle = read.supports_thinking_toggle === true;
  } catch {
    // An engine without the route, or a lost one: the next resync reads again.
  }
}

onResync(() => {
  refreshCapabilities();
});
onEvent("config_changed", () => {
  refreshCapabilities();
});

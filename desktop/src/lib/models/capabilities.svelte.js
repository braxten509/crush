// The main agent's capabilities (desktop-design-5-6 2.3, 7.4; GET
// agent/capabilities): its CLI kind, whether it can move work to the
// background (LIVE), and G-8's three toggle supports (the palette's FAST,
// Ultracode and Thinking entries). Read on every resync and again after
// config_changed, as a model change can change them.
import { api } from "../engine/api.js";
import { onEvent, onResync } from "../engine/events.js";

// An AgentCapabilities (lib/engine/wire.js).
export const capabilities = $state({ kind: "", can_background: false, supports_fast: false, supports_ultracode: false, supports_thinking_toggle: false });

let reading = 0;
export async function refreshCapabilities() {
  const seq = ++reading;
  try {
    const read = await api.capabilities();
    if (seq !== reading || !read || typeof read !== "object") return;
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

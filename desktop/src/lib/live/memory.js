// The memory warning on the notice line (desktop-design-5-6 2.17; LIVE-018,
// LIVE-020). GET /v1/memory when the window joins and when it rejoins, then
// the memory_status events, which the engine sends when percent, high or
// episode changes (engine design 5.9 c). While high, every reading shows the
// engine's message as a warning that stays until another notice replaces it
// (the terminal refreshes it at every check, internal/ui/model/
// memory_warning.go:24-49); it clears when high turns false, only while that
// text still shows. Where memory is not watched (supported: false, every
// system but Linux) nothing shows, and a failed read shows no error either:
// the memory box may simply be unavailable (LIVE-020). The one pop-up per
// episode is SYS's listener on the same event (claimMemoryNotice).
import { api } from "../engine/api.js";
import { onEvent, onResync } from "../engine/events.js";
import { clearNotice, notify } from "../stores/ui.svelte.js";

let shown = null; // the message this window put on the notice line, while high
let last = null; // the last MemoryStatus read (for the probe)

export function memoryStatus() {
  return last;
}

// One reading, from the route or an event.
export function applyMemory(status) {
  if (!status || typeof status !== "object") return;
  last = status;
  if (status.supported === true && status.high === true && status.message) {
    if (shown && shown !== status.message) clearNotice(shown);
    shown = status.message;
    notify("warn", status.message, 0);
    return;
  }
  if (shown !== null) {
    clearNotice(shown);
    shown = null;
  }
}

async function readMemory() {
  try {
    applyMemory(await api.memory());
  } catch {
    // No memory reading: nothing to warn about, and nothing to report (LIVE-020).
  }
}

let started = false;

export function startMemory() {
  if (started) return;
  started = true;
  onEvent("memory_status", (_kind, status) => applyMemory(status));
  // The first join reads it; a rejoin's resync reads it again below.
  onEvent("connection", (_kind, ev) => {
    if (ev?.state === "connected" && ev.resync !== true) readMemory();
  });
  onResync(() => readMemory());
}

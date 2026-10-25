// Why a provider cannot run in the shown folder, by provider id
// (engine op providers/availability, owner 2026-10-24: Moonshot AI only
// runs in Git projects). The model menu dims those rows and says why; the
// engine refuses the turn as well, so this is guidance, not the guard.
import { api } from "../engine/api.js";

// state: "loading" while the shown chat's folder is being checked, "ready"
// with its answer, "failed" when the read failed (owner's D4a: the rows stay
// pickable and say "Checking…" or "Could not check").
export const availability = $state({ sessionId: "", byProvider: {}, state: "loading" }); // provider id → reason
let sequence = 0;

// loadAvailability reads the folder's reasons. A failed read keeps the last
// answer for the same chat: no reason must never mean "this one can run".
export async function loadAvailability(sessionId = "") {
  const mine = ++sequence;
  if (availability.sessionId !== sessionId) {
    availability.sessionId = sessionId;
    availability.byProvider = {};
  }
  availability.state = "loading";
  try {
    const result = await api.providerAvailability(sessionId);
    if (mine !== sequence) return;
    availability.byProvider = result?.providers ?? {};
    availability.state = "ready";
  } catch (error) {
    if (mine !== sequence) return;
    availability.state = "failed";
    throw error;
  }
}

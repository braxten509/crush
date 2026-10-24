// Why a provider cannot run in the shown folder, by provider id
// (engine op providers/availability, owner 2026-10-24: Moonshot AI only
// runs in Git projects). The model menu dims those rows and says why; the
// engine refuses the turn as well, so this is guidance, not the guard.
import { api } from "../engine/api.js";

export const availability = $state({ byProvider: {} }); // provider id → reason

// loadAvailability reads the folder's reasons. A failed read leaves the
// last answer in place: no reason must never mean "this one can run".
export async function loadAvailability() {
  const result = await api.providerAvailability();
  availability.byProvider = result?.providers ?? {};
}

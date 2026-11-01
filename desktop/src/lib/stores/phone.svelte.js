// The composer's phone button (owner, 2026-11-01): shown while an Android
// phone is connected over adb, checked every few seconds while the window is
// on screen; otherwise a Find phone button looks for it on Wi-Fi. watchPhone() starts the checks and returns their stop.
import { phone_connected, phone_find } from "../engine/commands.js";
import { notify } from "./ui.svelte.js";

const EVERY_MS = 3000;

export const phone = $state({ connected: false, busy: false, finding: false });

const NOT_FOUND = "Couldn't find your phone. Is Wireless debugging on?";

// The Find phone button: looks on Wi-Fi and connects.
export async function findPhone() {
  if (phone.finding) return;
  phone.finding = true;
  try {
    phone.connected = Boolean(await phone_find());
    if (!phone.connected) notify("warn", NOT_FOUND);
  } catch (error) {
    notify("error", error?.message ?? String(error));
  } finally {
    phone.finding = false;
  }
}

export function watchPhone() {
  let stopped = false;
  let timer = null;
  const check = async () => {
    timer = null;
    if (stopped) return;
    if (document.visibilityState === "visible") {
      try {
        phone.connected = Boolean(await phone_connected());
      } catch {
        phone.connected = false;
      }
    }
    if (!stopped) timer = setTimeout(check, EVERY_MS);
  };
  check();
  return () => {
    stopped = true;
    if (timer) clearTimeout(timer);
  };
}

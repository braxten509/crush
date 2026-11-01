// The composer's phone button (owner, 2026-11-01): shown only while an
// Android phone is connected over adb, checked every few seconds while the
// window is on screen. watchPhone() starts the checks and returns their stop.
import { phone_connected } from "../engine/commands.js";

const EVERY_MS = 3000;

export const phone = $state({ connected: false, busy: false });

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

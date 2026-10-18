// The terminal's input grace for prompts that open by themselves (DLG-119,
// a port of OpenDialogWithGrace and inGracePeriod in
// internal/ui/dialog/dialog.go:46-55, 115-152, 175). While a prompt is in its
// grace, key presses are absorbed, so a key meant for the field cannot answer
// a prompt that has just appeared. The grace ends when no key has come for
// 425 ms, or 1.5 s after opening, whichever is first; each absorbed key
// restarts the quiet time. Reopening a prompt less than 500 ms after the same
// id closed has no grace, so quick successive prompts don't eat keys.
// Used by the permission prompt (P3.7) and by lib/dialogs.js for sheets opened
// with {grace: true}.

export const GRACE_QUIET_MS = 425;
export const GRACE_MAX_MS = 1500;
export const REOPEN_MS = 500;

const closedAt = new Map(); // id → when a prompt with that id last closed

// Keys that type nothing on their own; the terminal never sees them.
const MODIFIERS = new Set(["Shift", "Control", "Alt", "Meta", "AltGraph", "CapsLock", "Fn", "OS"]);

// A prompt or front sheet with this id closed now, with or without a grace
// (the terminal records every front dialog it removes, dialog.go:177-181).
export function noteClosed(id) {
  closedAt.set(id, Date.now());
}

export function createGrace(id) {
  const now = Date.now();
  const reopened = closedAt.has(id) && now - closedAt.get(id) < REOPEN_MS;
  let openedAt = reopened ? null : now;
  let lastInputAt = openedAt;

  function active(at) {
    return openedAt !== null && at - openedAt < GRACE_MAX_MS && at - lastInputAt < GRACE_QUIET_MS;
  }

  return {
    // For each keydown: true when the key must be swallowed.
    absorb(event) {
      if (MODIFIERS.has(event?.key)) return false;
      const at = Date.now();
      if (!active(at)) return false;
      lastInputAt = at;
      return true;
    },
    // Whether the prompt is still in its grace (for the probe and tests).
    active() {
      return active(Date.now());
    },
    // The prompt closed: remember when, for the reopen rule; the grace ends.
    close() {
      noteClosed(id);
      openedAt = null;
    },
  };
}

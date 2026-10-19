// The single click that opens or closes an item (CHAT-101): a left click
// with no drag between press and release toggles at once (owner, 2026-10-19:
// the old 400 ms wait for a possible double click made every row feel slow).
// The second and third clicks of a double or triple click toggle nothing, so
// they select a word or a line natively; the first click has toggled.
// A press and release this far apart (in CSS pixels) is a drag.
export const DRAG_DISTANCE = 4;

// singleClick(run) → {onpointerdown, onclick, cancel}: run(event) fires on a
// lone left click or a keyboard click. cancel is kept for callers; nothing
// waits any more.
export function singleClick(run) {
  let press = null;
  return {
    onpointerdown(event) {
      press = event.button === 0 ? { x: event.clientX, y: event.clientY } : null;
    },
    onclick(event) {
      // Enter or Space on a focused button clicks with detail 0.
      if (event.detail === 0) {
        press = null;
        run(event);
        return;
      }
      const dragged = !press || Math.hypot(event.clientX - press.x, event.clientY - press.y) > DRAG_DISTANCE;
      press = null;
      if (event.button !== 0 || event.detail > 1 || dragged) return;
      run(event);
    },
    cancel() {},
  };
}

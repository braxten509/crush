// The single click that opens or closes an item (CHAT-101): a left click
// with no drag between press and release and no second click within 400 ms
// (the double-click interval). A double or triple click selects a word or a
// line natively and toggles nothing.
export const DOUBLE_CLICK_MS = 400;
// A press and release this far apart (in CSS pixels) is a drag.
export const DRAG_DISTANCE = 4;

// singleClick(run) → {onpointerdown, onclick}: run(event) fires 400 ms after a
// lone click. timers: {set, clear} (setTimeout and clearTimeout by default).
export function singleClick(run, timers = { set: (fn, ms) => setTimeout(fn, ms), clear: (id) => clearTimeout(id) }) {
  let pending = null;
  let press = null;
  const cancel = () => {
    if (pending !== null) timers.clear(pending);
    pending = null;
  };
  return {
    onpointerdown(event) {
      press = event.button === 0 ? { x: event.clientX, y: event.clientY } : null;
    },
    onclick(event) {
      // Enter or Space on a focused button clicks with detail 0: no double
      // click to wait for.
      if (event.detail === 0) {
        cancel();
        press = null;
        run(event);
        return;
      }
      const dragged = !press || Math.hypot(event.clientX - press.x, event.clientY - press.y) > DRAG_DISTANCE;
      press = null;
      if (event.button !== 0 || event.detail > 1 || dragged) {
        cancel();
        return;
      }
      cancel();
      pending = timers.set(() => {
        pending = null;
        run(event);
      }, DOUBLE_CLICK_MS);
    },
    cancel,
  };
}

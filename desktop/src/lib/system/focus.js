// The window's focus, as SYS's alerts and badge read it (desktop-design-5-6
// 2.18 "State"): focused or not, and a list of listeners for a focus gain
// (NOTE-009 clears done and error then).
const gains = new Set();
export const windowFocus = { focused: typeof document === "undefined" ? false : document.hasFocus() };

export function onFocusGain(fn) {
  gains.add(fn);
  return () => gains.delete(fn);
}

let started = false;
export function startFocus(onChange) {
  if (started) return;
  started = true;
  window.addEventListener("focus", () => {
    windowFocus.focused = true;
    for (const fn of gains) fn();
    onChange?.();
  });
  window.addEventListener("blur", () => {
    windowFocus.focused = false;
    onChange?.();
  });
}

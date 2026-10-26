// Every scroll bar in the app hides until it is used (owner, 2026-10-26:
// "hide after a few seconds of not using it as well as not showing in general
// until it was used ... app wide"). A scroll the user made (a wheel turn,
// key or press in the last INPUT_MS), or a press on an area's bar, puts
// bar-used on it; it goes SHOW_MS after the last scroll, or after the press
// ends. The app moving an area itself (a reply following the bottom, a
// resize) shows nothing. app.css paints the thumb only on bar-used (or with
// the Settings choice "always"), so the bar's room stays and text never
// moves. The chat (VirtualList, data-bar-fade="own") and the sheets
// (kit/Scrollbar, .kit-scroll) fade their own bar, asking userActed().
//
//   startBarFade(doc) listens on doc; returns the stop function
//   userActed()       whether the user turned the wheel, typed or pressed
//                     (or dragged) within the last INPUT_MS

export const SHOW_MS = 2000;
export const INPUT_MS = 600;
const OWN = '.kit-scroll, [data-bar-fade="own"]';

let lastInput = -Infinity;

export function userActed() {
  return Date.now() - lastInput < INPUT_MS;
}

export function startBarFade(doc = document) {
  const timers = new Map();
  let held = null;

  function hideLater(el) {
    clearTimeout(timers.get(el));
    timers.set(
      el,
      setTimeout(() => {
        timers.delete(el);
        if (el !== held) el.classList.remove("bar-used");
      }, SHOW_MS),
    );
  }

  function show(el) {
    el.classList.add("bar-used");
    hideLater(el);
  }

  // Scroll does not bubble, so it is caught on the way down.
  function onScroll(event) {
    const el = event.target === doc ? doc.scrollingElement : event.target;
    if (!el?.classList || el.matches(OWN)) return;
    if (el === held || userActed()) show(el);
  }

  function onInput(event) {
    if (event.type !== "pointermove" || event.buttons) lastInput = Date.now();
  }

  // A press on an area's bar column (right of its content, or under it).
  function onDown(event) {
    const el = event.target;
    if (event.button !== 0 || !el?.classList || el.matches(OWN)) return;
    const onSide = el.offsetWidth > el.clientWidth && event.offsetX >= el.clientWidth;
    const onFoot = el.offsetHeight > el.clientHeight && event.offsetY >= el.clientHeight;
    if (!onSide && !onFoot) return;
    held = el;
    show(el);
    doc.defaultView.addEventListener(
      "mouseup",
      () => {
        held = null;
        hideLater(el);
      },
      { once: true, capture: true },
    );
  }

  const inputs = ["wheel", "keydown", "pointerdown", "pointermove", "touchmove"];
  for (const type of inputs) doc.addEventListener(type, onInput, { capture: true, passive: true });
  doc.addEventListener("scroll", onScroll, { capture: true, passive: true });
  doc.addEventListener("mousedown", onDown, { capture: true });
  return () => {
    for (const type of inputs) doc.removeEventListener(type, onInput, { capture: true });
    doc.removeEventListener("scroll", onScroll, { capture: true });
    doc.removeEventListener("mousedown", onDown, { capture: true });
    for (const timer of timers.values()) clearTimeout(timer);
    timers.clear();
  };
}

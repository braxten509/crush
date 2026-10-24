const HIDE_AFTER_MS = 2000;

// Reveal native scrollbars for any overflowing element during scroll, then
// hide them again after input stops. Scroll events do not bubble, so listen
// in the capture phase at the document.
export function startScrollbars(doc = document) {
  const timers = new Map();

  function onScroll(event) {
    const element = event.target;
    if (!(element instanceof doc.defaultView.Element)) return;
    if (element.scrollHeight <= element.clientHeight && element.scrollWidth <= element.clientWidth) return;

    element.classList.add("app-scrollbar-active");
    clearTimeout(timers.get(element));
    timers.set(
      element,
      setTimeout(() => {
        element.classList.remove("app-scrollbar-active");
        timers.delete(element);
      }, HIDE_AFTER_MS),
    );
  }

  doc.addEventListener("scroll", onScroll, { capture: true, passive: true });
  return () => {
    doc.removeEventListener("scroll", onScroll, { capture: true });
    for (const [element, timer] of timers) {
      clearTimeout(timer);
      element.classList.remove("app-scrollbar-active");
    }
    timers.clear();
  };
}

// Where the timeline sits on screen, for the sheets drawn over it from the
// overlay layer (4.14): the help sheet hangs from the timeline's bottom, the
// details overlay drops from under the head rule. Read from the conversation
// column (S3's <main class="conversation">), its head rule (.rule.under-head)
// and the notice line's anchor, which sits on the timeline's bottom edge
// (NoticeLine). Viewport pixels; null while the conversation is not drawn.
export function timelineBox(doc = document) {
  const column = doc.querySelector("main.conversation");
  const head = column?.querySelector(".rule.under-head");
  const anchor = column?.querySelector("[data-notice-anchor]");
  if (!column || !anchor) return null;
  const c = column.getBoundingClientRect();
  // A chat has no head (owner, 2026-10-20): the timeline starts at the column's top.
  return { left: c.left, width: c.width, top: head ? head.getBoundingClientRect().bottom : c.top, bottom: anchor.getBoundingClientRect().top };
}

// Calls update now and whenever the box may have moved: window resizes and
// zoom changes, the column's size, and the timeline's (the tray or the
// composer growing moves its bottom). Returns a stop function.
export function trackBox(update, doc = document) {
  const win = doc.defaultView;
  update();
  const observer = new ResizeObserver(update);
  const column = doc.querySelector("main.conversation");
  if (column) observer.observe(column);
  const timeline = column?.querySelector("[data-notice-anchor]")?.previousElementSibling;
  if (timeline) observer.observe(timeline);
  win.addEventListener("resize", update);
  return () => {
    observer.disconnect();
    win.removeEventListener("resize", update);
  };
}

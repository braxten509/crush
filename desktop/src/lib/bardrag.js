// The chat's scrollbar thumb, dragged by the page itself (owner, 2026-10-26,
// picked "write our own drag"). The system's bar moves its thumb on the
// browser's own thread by the mouse's steps, while the chat grows as it
// measures older messages, so a drag to the top of a long chat stopped a
// quarter of the way down. Here the mouse's place maps onto the chat's length
// as it is at each move, so the ends are always reached.
//
//   barThumb({height, client, total, top}) the thumb in the scroller (its
//                          top edge at 0): {start, length, travel}, as
//                          Chromium's Fluent bar draws it: arrows ARROW long,
//                          the thumb at least MIN_THUMB (measured 2026-10-26)
//   dragTop({frac, dy, travel, range}) the scrollTop for a drag that began
//                          at frac of the way down and has moved dy since

export const ARROW = 18;
export const MIN_THUMB = 17;

export function barThumb({ height, client, total, top }) {
  const track = Math.max(0, height - 2 * ARROW);
  const length = Math.min(track, Math.max(MIN_THUMB, (track * client) / Math.max(1, total)));
  const travel = track - length;
  const range = Math.max(1, total - client);
  return { start: ARROW + Math.min(1, Math.max(0, top / range)) * travel, length, travel };
}

export function dragTop({ frac, dy, travel, range }) {
  const at = Math.min(1, Math.max(0, frac + dy / Math.max(1, travel)));
  return at * range;
}

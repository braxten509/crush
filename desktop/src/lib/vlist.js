// The virtual list's arithmetic (desktop-design 1.6 items 2 and 8; CHAT-087,
// CHAT-092, CHAT-099, CHAT-105). Pure: VirtualList.svelte keeps the page and
// the state, these functions place the items. Lengths are real pixels except
// where a name says "picture px" (multiply by the zoom).
//
//   gapBefore(prev, item)            the picture's rhythm between items (picture px)
//   estimate(item)                   a height for an item never measured (picture px)
//   createHeights()                  measured heights by key, version and width
//   layout(items, heightOf, gapOf, pad)
//                                    → {count, tops, heights, gaps, total, pad}:
//                                    tops[i] is where item i's slot starts (its gap
//                                    first), so item i is drawn from tops[i] + gaps[i]
//   windowRange(lay, top, viewport)  the items to render: the viewport and one
//                                    viewport above and below
//   mountedIndices(range, pinned)    that range plus the pinned items (the two
//                                    items holding a selection's ends), sorted
//   segments(lay, mounted)           spacers and items in page order
//   visibleRange(lay, top, viewport) the first and last item in view
//   inView(lay, i, top, viewport)
//   anchorAt(lay, keys, top) / topForAnchor(lay, keys, anchor)
//                                    the first item in view and how far into it
//                                    the view starts: what stays still when items
//                                    above change height
//   scrollToShow(lay, i, top, viewport)   the least scroll that shows item i
//   atBottom(top, viewport, total)   an item taller than the view counts as at the
//                                    bottom once its end is shown (CHAT-092)

// -- The rhythm between items (the picture's, Timeline.svelte as built) --

const toolsLike = (kind) => kind === "tools" || kind === "tool";
const userLike = (kind) => kind === "user" || kind === "pending";

// 40 by default, more air after your own message, a tool run a little closer
// to the reply that ran it (37 before it, 39 after it); a reply's own banner,
// live status and footer sit 16 under it.
export function gapBefore(prev, item) {
  if (!prev) return 0;
  const ownPart = item.kind === "banner" || item.kind === "status" || item.kind === "footer";
  if (ownPart && prev.messageId && prev.messageId === item.messageId) return 16;
  if (toolsLike(prev.kind)) return 39;
  if (toolsLike(item.kind)) return 37;
  if (userLike(prev.kind)) return 44;
  return 40;
}

// -- Estimates (1.6 item 2: user 80, reply 33 per 70 characters plus 50, tool
//    group 44) --

export function estimate(item) {
  switch (item.kind) {
    case "user":
      return 80 + (item.chips?.length ? 42 : 0);
    case "pending":
      return 80 + (item.chips?.length ? 42 : 0) + (item.state === "uncertain" ? 32 : 0);
    case "reply":
      return 33 * Math.max(1, Math.ceil((item.text?.length ?? 0) / 70)) + 50;
    case "tools":
    case "tool":
      return 44;
    case "shell":
      return 120;
    case "banner":
      if (item.banner === "error" || item.banner === "refused") return 26 + (item.details ? 8 + 24 * Math.ceil(item.details.length / 80) : 0);
      return 24;
    case "notice":
    case "footer":
    case "status":
      return 24;
    default:
      return 40;
  }
}

// -- Measured heights --

// The last measured height of each item. An exact hit has the item's version
// and the column's width; any other measure of the same key is still a better
// guess than the estimate (a streaming reply grows from its last height).
export function createHeights() {
  const byKey = new Map(); // key → {version, width, h}
  return {
    get(item, width) {
      const m = byKey.get(item.key);
      return m ? m.h : null;
    },
    exact(item, width) {
      const m = byKey.get(item.key);
      return Boolean(m) && m.version === item.version && m.width === width;
    },
    // True when the height changed.
    set(item, width, h) {
      const m = byKey.get(item.key);
      if (m && m.version === item.version && m.width === width && Math.abs(m.h - h) < 0.01) return false;
      byKey.set(item.key, { version: item.version, width, h });
      return !m || Math.abs(m.h - h) >= 0.01;
    },
    // Forgets keys that left the chat.
    keep(keys) {
      for (const key of [...byKey.keys()]) if (!keys.has(key)) byKey.delete(key);
    },
    get size() {
      return byKey.size;
    },
  };
}

// -- Layout --

export function layout(items, heightOf, gapOf, pad = { top: 0, bottom: 0 }) {
  const count = items.length;
  const tops = new Float64Array(count + 1);
  const heights = new Float64Array(count);
  const gaps = new Float64Array(count);
  let y = pad.top;
  for (let i = 0; i < count; i++) {
    tops[i] = y;
    gaps[i] = gapOf(i);
    heights[i] = heightOf(i);
    y += gaps[i] + heights[i];
  }
  tops[count] = y;
  return { count, tops, heights, gaps, total: y + pad.bottom, pad };
}

const itemTop = (lay, i) => lay.tops[i] + lay.gaps[i];
const itemBottom = (lay, i) => lay.tops[i + 1];

// The first index whose bottom is below y (count when none).
function firstEndingAfter(lay, y) {
  let lo = 0;
  let hi = lay.count;
  while (lo < hi) {
    const mid = (lo + hi) >> 1;
    if (itemBottom(lay, mid) > y) hi = mid;
    else lo = mid + 1;
  }
  return lo;
}

// The last index whose top is above y (-1 when none).
function lastStartingBefore(lay, y) {
  let lo = -1;
  let hi = lay.count - 1;
  while (lo < hi) {
    const mid = (lo + hi + 1) >> 1;
    if (itemTop(lay, mid) < y) lo = mid;
    else hi = mid - 1;
  }
  return lo;
}

// The items in view: {first, last}, or null when none is.
export function visibleRange(lay, top, viewport) {
  if (lay.count === 0) return null;
  const first = firstEndingAfter(lay, top);
  const last = lastStartingBefore(lay, top + viewport);
  return first <= last ? { first, last } : null;
}

export function inView(lay, i, top, viewport) {
  return i >= 0 && i < lay.count && itemBottom(lay, i) > top && itemTop(lay, i) < top + viewport;
}

// The items to render: those within one viewport above and below the view.
export function windowRange(lay, top, viewport, overscan = viewport) {
  if (lay.count === 0) return null;
  const first = Math.min(lay.count - 1, firstEndingAfter(lay, top - overscan));
  const last = Math.max(first, lastStartingBefore(lay, top + viewport + overscan));
  return { first, last: Math.min(last, lay.count - 1) };
}

// The window plus the pinned indexes (out-of-range ones dropped), sorted.
export function mountedIndices(range, pinned = []) {
  const out = new Set();
  if (range) for (let i = range.first; i <= range.last; i++) out.add(i);
  for (const i of pinned) if (Number.isInteger(i) && i >= 0) out.add(i);
  return [...out].sort((a, b) => a - b);
}

// Spacers for the runs of items not rendered, and the rendered items, in page
// order: [{spacer: true, height, id}] and [{index}].
export function segments(lay, mounted) {
  const out = [];
  let next = 0;
  for (const i of mounted) {
    if (i >= lay.count) break;
    if (i > next) out.push({ spacer: true, height: lay.tops[i] - lay.tops[next], id: `gap:${next}` });
    out.push({ index: i });
    next = i + 1;
  }
  if (next < lay.count) out.push({ spacer: true, height: lay.tops[lay.count] - lay.tops[next], id: `gap:${next}` });
  return out;
}

// -- Keeping the view still --

// The first item whose slot reaches into the view, by key, and how far the
// view starts below that slot's top.
export function anchorAt(lay, keys, top) {
  if (lay.count === 0) return null;
  const i = Math.min(lay.count - 1, firstEndingAfter(lay, top));
  return { key: keys[i], offset: top - lay.tops[i] };
}

// The scroll top that puts the anchor where it was; null when its item left.
export function topForAnchor(lay, keys, anchor) {
  if (!anchor) return null;
  const i = keys.indexOf(anchor.key);
  return i < 0 ? null : lay.tops[i] + anchor.offset;
}

export function maxTop(lay, viewport) {
  return Math.max(0, lay.total - viewport);
}

export function clampTop(lay, viewport, top) {
  return Math.min(maxTop(lay, viewport), Math.max(0, top));
}

// The least scroll that shows item i: its top when it is above the view or
// taller than it, its bottom at the view's bottom when it is below.
export function scrollToShow(lay, i, top, viewport) {
  if (i < 0 || i >= lay.count) return top;
  const t = itemTop(lay, i);
  const b = itemBottom(lay, i);
  let next = top;
  if (t < top || b - t > viewport) next = t;
  else if (b > top + viewport) next = b - viewport;
  return clampTop(lay, viewport, next);
}

export function atBottom(top, viewport, total) {
  return top + viewport >= total - 1;
}

// NEAR_BOTTOM is about two reading lines, in CSS pixels at zoom 1.
const NEAR_BOTTOM = 40;

// Whether the view is within about two lines of the bottom: there, a scroll
// keeps following, so a touchpad's tiny nudge doesn't leave the next reply
// out of view (owner, 2026-10-22).
export function nearBottom(top, viewport, total, zoom = 1) {
  return top + viewport >= total - NEAR_BOTTOM * zoom;
}

export { itemTop, itemBottom };

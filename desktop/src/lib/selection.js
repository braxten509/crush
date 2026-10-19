// The timeline's two selections (desktop-design 1.6 item 8, 1.8; CHAT-003,
// CHAT-087, CHAT-096, CHAT-100, CHAT-101): the selected item (one at most,
// ui.selectedItem) and the page's text selection, whose two end items stay
// mounted. The terminal's rules for which items take the selection and where
// it moves are ported from internal/ui/model/chat_scroll.go:132-269.
//
//   isSelectable(item)                 items a click or a key can select
//   nextSelectable(items, i, dir)      the selectable item after (dir 1) or before (-1) index i
//   firstSelectable / lastSelectable(items, from, to)
//   carrySelection(key, items)         where a selection goes when its item left:
//                                      the item that took it over (a group holding
//                                      a tool, the saved copy of a pending prompt),
//                                      else null
//   keyAtPoint(node, offset, side)     the item key at a point of the page
//   timelineRange(root)                the page's selection when it is inside root
//   selectionEnds(range)               {startKey, endKey}: the items to keep mounted
//   selectAcross(first, last)          selects from the first frame's start to the
//                                      last frame's end (Ctrl+A, CHAT-087)
//   expandedOf(frame)                  the item's open state from its toggle's
//                                      aria-expanded, or null when it has none

// The terminal selects focusable items: messages, notices, tools and shells;
// a footer, a banner or a live status is part of a message and never selected
// on its own (chat_scroll.go:132-139).
const SELECTABLE = new Set(["user", "pending", "reply", "notice", "tools", "tool", "shell"]);

export function isSelectable(item) {
  return Boolean(item) && SELECTABLE.has(item.kind);
}

export function nextSelectable(items, i, dir) {
  for (let k = i + dir; k >= 0 && k < items.length; k += dir) if (isSelectable(items[k])) return k;
  return -1;
}

export function firstSelectable(items, from = 0, to = items.length - 1) {
  for (let k = Math.max(0, from); k <= Math.min(to, items.length - 1); k++) if (isSelectable(items[k])) return k;
  return -1;
}

export function lastSelectable(items, from = 0, to = items.length - 1) {
  for (let k = Math.min(to, items.length - 1); k >= Math.max(0, from); k--) if (isSelectable(items[k])) return k;
  return -1;
}

// A selected key that is gone: a tool that folded into a group goes to its
// group, a call shown in a group stays with the group's members; anything
// else is dropped (the key handoffs of lib/timeline.js keep the rest).
export function carrySelection(key, items) {
  if (!key) return null;
  if (items.some((item) => item.key === key)) return key;
  const id = key.startsWith("tool:") ? key.slice(5) : key;
  const holder = items.find((item) => item.kind === "tools" && item.members?.includes(id));
  return holder ? holder.key : null;
}

// -- The page's text selection --

function keyOf(el) {
  return el?.closest?.("[data-key]")?.dataset.key ?? null;
}

function firstKeyIn(node) {
  if (node.nodeType !== 1) return null;
  if (node.dataset?.key) return node.dataset.key;
  return node.querySelector?.("[data-key]")?.dataset.key ?? null;
}

function lastKeyIn(node) {
  if (node.nodeType !== 1) return null;
  if (node.dataset?.key) return node.dataset.key;
  const all = node.querySelectorAll?.("[data-key]");
  return all && all.length ? all[all.length - 1].dataset.key : null;
}

// The item holding a point; a point between items gives the item after it
// (side "start") or before it (side "end").
export function keyAtPoint(node, offset, side = "start") {
  if (!node) return null;
  const el = node.nodeType === 1 ? node : node.parentElement;
  const own = keyOf(el);
  if (own) return own;
  if (node.nodeType !== 1) return null;
  if (side === "start") {
    for (let c = node.childNodes[offset] ?? null; c; c = c.nextSibling) {
      const k = firstKeyIn(c);
      if (k) return k;
    }
  } else {
    for (let c = node.childNodes[offset - 1] ?? null; c; c = c.previousSibling) {
      const k = lastKeyIn(c);
      if (k) return k;
    }
  }
  return null;
}

// The page's selection as a range when it is not collapsed and both its ends
// lie inside root; else null.
export function timelineRange(root) {
  const selection = root?.ownerDocument?.defaultView?.getSelection?.();
  if (!selection || selection.isCollapsed || selection.rangeCount === 0) return null;
  const range = selection.getRangeAt(0);
  if (!root.contains(range.startContainer) || !root.contains(range.endContainer)) return null;
  return range;
}

export function selectionEnds(range) {
  if (!range) return { startKey: null, endKey: null };
  return {
    startKey: keyAtPoint(range.startContainer, range.startOffset, "start"),
    endKey: keyAtPoint(range.endContainer, range.endOffset, "end"),
  };
}

// Selects from the start of the first frame to the end of the last.
export function selectAcross(first, last) {
  const selection = first?.ownerDocument?.defaultView?.getSelection?.();
  if (!selection || !last) return false;
  selection.setBaseAndExtent(first, 0, last, last.childNodes.length);
  return true;
}

// -- Open state --

// An item's toggle (its first control with aria-expanded): a tool group's
// row, a tool's line, a shell's output.
export function toggleOf(frame) {
  return frame?.querySelector?.("[aria-expanded]") ?? null;
}

export function expandedOf(frame) {
  const toggle = toggleOf(frame);
  if (!toggle) return null;
  return toggle.getAttribute("aria-expanded") === "true";
}

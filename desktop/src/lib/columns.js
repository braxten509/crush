// Column widths. The chat list (left) and the side panel (right) have widths in
// picture pixels; the conversation takes what is left. Dragging a divider resizes
// the column beside it and the conversation adjusts; the widths are remembered.

export const COLUMN_DEFAULTS = { left: 389, right: 453 };
export const COLUMN_LIMITS = {
  left: { min: 280, max: 560 },
  right: { min: 320, max: 680 },
  middleMin: 460,
};
const KEY = "crush.columns";

function clamp(value, min, max) {
  return Math.min(max, Math.max(min, value));
}

// Keeps both side columns in range and leaves the conversation at least its minimum,
// given the inner width of the page in picture pixels (null when not known yet).
// mins raises a side's minimum to what its words need: the panel's meters
// (Meters.svelte), the list's New chat and New terminal (ChatList.svelte)
// (owner, 2026-10-20: no words overlap or are cut).
export function fitColumns(widths, inner = null, mins = {}) {
  const leftLow = Math.max(COLUMN_LIMITS.left.min, Math.ceil(mins.left ?? 0));
  const low = Math.max(COLUMN_LIMITS.right.min, Math.ceil(mins.right ?? 0));
  let left = clamp(Math.round(widths.left), leftLow, Math.max(leftLow, COLUMN_LIMITS.left.max));
  let right = clamp(Math.round(widths.right), low, Math.max(low, COLUMN_LIMITS.right.max));
  if (inner !== null) {
    const spare = inner - COLUMN_LIMITS.middleMin;
    if (left + right > spare) {
      // Shrink the side that is furthest above its minimum first.
      let over = left + right - spare;
      const leftRoom = left - leftLow;
      const rightRoom = right - low;
      const fromRight = Math.min(rightRoom, Math.max(0, over - leftRoom), over);
      right -= fromRight;
      over -= fromRight;
      left -= Math.min(leftRoom, over);
    }
  }
  return { left, right };
}

// Dragging a divider by `delta` picture pixels (positive is to the right).
export function dragColumn(widths, side, delta, inner = null, mins = {}) {
  if (side === "left") return fitColumns({ ...widths, left: widths.left + delta }, inner, mins);
  return fitColumns({ ...widths, right: widths.right - delta }, inner, mins);
}

export function loadColumns(storage) {
  try {
    const saved = JSON.parse(storage?.getItem(KEY) ?? "null");
    if (saved && Number.isFinite(saved.left) && Number.isFinite(saved.right)) return fitColumns(saved);
  } catch {
    // Unreadable or missing: use the defaults.
  }
  return { ...COLUMN_DEFAULTS };
}

export function saveColumns(storage, widths) {
  try {
    storage?.setItem(KEY, JSON.stringify({ left: widths.left, right: widths.right }));
  } catch {
    // Not remembered this time; the widths still apply.
  }
}

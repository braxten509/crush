// How many of the right panel's usage meters share one row.
//
// Three at most, but four go two and two: 3 on top with 1 alone below
// looked lopsided next to the rest of the panel (owner, 2026-10-24).
export function metersPerRow(count) {
  if (count === 4) return 2;
  return Math.min(3, Math.max(1, count));
}

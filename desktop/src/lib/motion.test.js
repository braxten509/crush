import { describe, expect, test } from "vitest";
import { slidePage } from "./motion.js";

// The clip-path a slide sets at u (0 = in place, 1 = fully moved).
function clipAt(dir, direction, u) {
  const css = slidePage(null, { dir })({ direction }).css(1 - u, u);
  const clips = [...css.matchAll(/clip-path:\s*([^;]+);/g)].map((m) => m[1].trim());
  return clips.at(-1) ?? "";
}

// The clip's four sides as numbers (px or %), from inset(top right bottom left).
function insetAt(dir, direction, u) {
  const match = /inset\(([^)]*)\)/.exec(clipAt(dir, direction, u));
  expect(match).not.toBeNull();
  const [top, right = top, bottom = top, left = right] = match[1].trim().split(/\s+/).map((v) => Number.parseFloat(v));
  return { top, right, bottom, left };
}

// A sliding page stops at its own edge: the part that has moved past its box
// is cut off, so it never paints over the column's frame lines (owner,
// 2026-10-27: Settings' list covered the left and top lines going back).
describe("slidePage keeps to its own box", () => {
  test("coming in from the left, the part left of its box is cut off", () => {
    const inset = insetAt(-1, "in", 0.5);
    expect(inset.left).toBeGreaterThan(0);
    expect(inset.right).toBe(0);
  });
  test("coming in from the right, the part right of its box is cut off", () => {
    const inset = insetAt(1, "in", 0.5);
    expect(inset.right).toBeGreaterThan(0);
    expect(inset.left).toBe(0);
  });
  test("leaving to the left, it rolls away and is cut at its left edge", () => {
    expect(insetAt(-1, "out", 0.5).left).toBeGreaterThan(0);
  });
  test("leaving to the right, it rolls away and is cut at its right edge", () => {
    expect(insetAt(1, "out", 0.5).right).toBeGreaterThan(0);
  });
});

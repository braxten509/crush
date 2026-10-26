import { describe, expect, it } from "vitest";
import { barThumb, dragTop } from "./bardrag.js";

// A chat 828 tall showing 828 of 84 940, its bar's arrows 18 long each.
const bar = { height: 828, client: 828, total: 84940 };

describe("the chat bar's own drag (bardrag.js)", () => {
  it("places the thumb between the arrows, never shorter than the least length", () => {
    expect(barThumb({ ...bar, top: 0 })).toEqual({ start: 18, length: 17, travel: 775 });
    const end = barThumb({ ...bar, top: 84940 - 828 });
    expect(end.start + end.length).toBeCloseTo(828 - 18, 6);
    expect(barThumb({ height: 400, client: 400, total: 800, top: 0 }).length).toBe(182);
  });

  it("maps the drag on today's length, so the ends are always reached", () => {
    // Half way, then the mouse goes up past the track: the very top.
    expect(dragTop({ frac: 0.5, dy: -500, travel: 781, range: 90000 })).toBe(0);
    // Down past it: the very bottom of the chat as long as it is now.
    expect(dragTop({ frac: 0.5, dy: 500, travel: 781, range: 120000 })).toBe(120000);
    // A quarter of the travel up from half way: a quarter of the way down.
    expect(dragTop({ frac: 0.5, dy: -781 / 4, travel: 781, range: 100000 })).toBeCloseTo(25000, 6);
  });
});

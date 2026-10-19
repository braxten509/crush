import { afterEach, describe, expect, it, vi } from "vitest";
import { frameAt, secondAt, ticker } from "./ticker.svelte.js";

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe("the ticker (desktop-design-5-6 2.20, look 5.2)", () => {
  it("steps the frame every 100 ms of the wall clock, eight frames round", () => {
    expect([0, 99, 100, 799, 800].map(frameAt)).toEqual([0, 0, 1, 7, 0]);
  });

  it("steps the second every 1000 ms, four round", () => {
    expect([0, 999, 1000, 3999, 4000].map(secondAt)).toEqual([0, 0, 1, 3, 0]);
  });

  it("reads the wall clock, so every window shows the same frame", () => {
    vi.useFakeTimers();
    vi.setSystemTime(12_345);
    expect([ticker.frame, ticker.second]).toEqual([3, 0]);
  });

  it("stands still at 0 under reduced motion", () => {
    vi.useFakeTimers();
    vi.setSystemTime(12_345);
    vi.stubGlobal("matchMedia", (query) => ({ matches: query === "(prefers-reduced-motion: reduce)" }));
    expect([ticker.frame, ticker.second]).toEqual([0, 0]);
  });
});

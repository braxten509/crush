import { describe, expect, it } from "vitest";
import { metersPerRow } from "./metergrid.js";

// The right panel's meters share a row: 3 at most, but 4 go two and two
// rather than 3 on top and 1 alone below (owner, 2026-10-24).
describe("metersPerRow", () => {
  it("puts four meters two on each row", () => {
    expect(metersPerRow(4)).toBe(2);
  });

  it("keeps three on a row when that leaves no one alone", () => {
    expect(metersPerRow(3)).toBe(3);
    expect(metersPerRow(5)).toBe(3);
    expect(metersPerRow(6)).toBe(3);
  });

  it("gives one or two meters the whole row", () => {
    expect(metersPerRow(1)).toBe(1);
    expect(metersPerRow(2)).toBe(2);
  });

  it("never gives a row fewer than one meter", () => {
    expect(metersPerRow(0)).toBe(1);
  });
});

import { describe, expect, it } from "vitest";
import { COLUMN_DEFAULTS, COLUMN_LIMITS, dragColumn, fitColumns, loadColumns, saveColumns } from "./columns.js";

const memory = (value = null) => {
  let stored = value;
  return { getItem: () => stored, setItem: (_k, v) => { stored = v; }, read: () => stored };
};

describe("columns", () => {
  it("starts at the picture's widths", () => {
    expect(loadColumns(memory())).toEqual(COLUMN_DEFAULTS);
    expect(loadColumns(memory("not json"))).toEqual(COLUMN_DEFAULTS);
  });

  it("remembers widths", () => {
    const store = memory();
    saveColumns(store, { left: 300, right: 400 });
    expect(loadColumns(store)).toEqual({ left: 300, right: 400 });
  });

  it("drags the left divider right to widen the chat list", () => {
    expect(dragColumn(COLUMN_DEFAULTS, "left", 40)).toEqual({ left: 429, right: 453 });
  });

  it("drags the right divider left to widen the side panel", () => {
    expect(dragColumn(COLUMN_DEFAULTS, "right", -40)).toEqual({ left: 389, right: 493 });
  });

  it("never drags a side narrower than its words need", () => {
    expect(dragColumn(COLUMN_DEFAULTS, "right", 999, null, { right: 512 }).right).toBe(512);
    expect(dragColumn(COLUMN_DEFAULTS, "left", -999, null, { left: 330 }).left).toBe(330);
    // Wider than the usual maximum: the words still win.
    expect(fitColumns({ left: 389, right: 900 }, null, { right: 700 }).right).toBe(700);
    // Short of room, each side stops at its raised minimum.
    const tight = fitColumns({ left: 389, right: 600 }, 330 + 512 + COLUMN_LIMITS.middleMin, { left: 330, right: 512 });
    expect(tight).toEqual({ left: 330, right: 512 });
  });

  it("keeps each side column inside its limits", () => {
    expect(dragColumn(COLUMN_DEFAULTS, "left", -999).left).toBe(COLUMN_LIMITS.left.min);
    expect(dragColumn(COLUMN_DEFAULTS, "right", -999).right).toBe(COLUMN_LIMITS.right.max);
  });

  it("never squeezes the conversation below its minimum", () => {
    const inner = 389 + 453 + COLUMN_LIMITS.middleMin + 20;
    const wide = dragColumn(COLUMN_DEFAULTS, "left", 200, inner);
    expect(inner - wide.left - wide.right).toBeGreaterThanOrEqual(COLUMN_LIMITS.middleMin);
    const small = fitColumns(COLUMN_DEFAULTS, 1000);
    expect(small.left).toBeGreaterThanOrEqual(COLUMN_LIMITS.left.min);
    expect(small.right).toBeGreaterThanOrEqual(COLUMN_LIMITS.right.min);
  });
});

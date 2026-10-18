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

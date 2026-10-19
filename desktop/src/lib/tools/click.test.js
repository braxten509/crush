// CHAT-101 for tool rows: a lone click toggles at once; the extra clicks of a
// double click, a drag or another button toggle nothing;
// CHAT-090: the copy notice and its failure.
import { describe, expect, it } from "vitest";
import { copyWithNotice, COPY_FAILED } from "./clip.js";
import { singleClick } from "./click.js";

const at = (x, y, fields = {}) => ({ button: 0, clientX: x, clientY: y, detail: 1, ...fields });

describe("single click", () => {
  it("toggles at once on a lone click", () => {
    const runs = [];
    const click = singleClick(() => runs.push("toggle"));
    click.onpointerdown(at(10, 10));
    click.onclick(at(11, 10));
    expect(runs).toEqual(["toggle"]);
  });
  it("toggles once for a double click: the second click only selects", () => {
    const runs = [];
    const click = singleClick(() => runs.push("toggle"));
    click.onpointerdown(at(10, 10));
    click.onclick(at(10, 10));
    click.onpointerdown(at(10, 10));
    click.onclick(at(10, 10, { detail: 2 }));
    expect(runs).toEqual(["toggle"]);
  });
  it("does nothing after a drag or for another button", () => {
    const runs = [];
    const click = singleClick(() => runs.push("toggle"));
    click.onpointerdown(at(10, 10));
    click.onclick(at(60, 10));
    click.onpointerdown(at(10, 10, { button: 1 }));
    click.onclick(at(10, 10, { button: 1 }));
    expect(runs).toEqual([]);
  });
  it("runs at once for a keyboard click", () => {
    const runs = [];
    singleClick(() => runs.push("toggle")).onclick({ button: 0, clientX: 0, clientY: 0, detail: 0 });
    expect(runs).toEqual(["toggle"]);
  });
});

describe("copy notices", () => {
  it("shows the row's notice after a good write, a warning after a failed one", async () => {
    const told = [];
    const tell = (kind, text) => told.push([kind, text]);
    const written = [];
    expect(await copyWithNotice("x", "Tool content copied to clipboard", async (t) => written.push(t), tell)).toBe(true);
    expect(await copyWithNotice("y", "Shell output copied to clipboard", async () => Promise.reject(new Error("no")), tell)).toBe(false);
    expect(written).toEqual(["x"]);
    expect(told).toEqual([
      ["info", "Tool content copied to clipboard"],
      ["warn", COPY_FAILED],
    ]);
    expect(COPY_FAILED).toBe("Failed to copy to clipboard");
  });
});

// CHAT-101 for tool rows: a lone click toggles after the double-click
// interval; a double click, a drag or another button toggles nothing;
// CHAT-090: the copy notice and its failure.
import { describe, expect, it } from "vitest";
import { copyWithNotice, COPY_FAILED } from "./clip.js";
import { DOUBLE_CLICK_MS, singleClick } from "./click.js";

function fakeTimers() {
  const due = new Map();
  let next = 1;
  return {
    set(fn, ms) {
      due.set(next, { fn, ms });
      return next++;
    },
    clear(id) {
      due.delete(id);
    },
    run() {
      const all = [...due.values()];
      due.clear();
      for (const t of all) t.fn();
      return all.map((t) => t.ms);
    },
  };
}

const at = (x, y, fields = {}) => ({ button: 0, clientX: x, clientY: y, detail: 1, ...fields });

describe("single click", () => {
  it("toggles once, after the double-click interval", () => {
    const timers = fakeTimers();
    const runs = [];
    const click = singleClick(() => runs.push("toggle"), timers);
    click.onpointerdown(at(10, 10));
    click.onclick(at(11, 10));
    expect(runs).toEqual([]);
    expect(timers.run()).toEqual([DOUBLE_CLICK_MS]);
    expect(runs).toEqual(["toggle"]);
    expect(DOUBLE_CLICK_MS).toBe(400);
  });
  it("does nothing for a double click", () => {
    const timers = fakeTimers();
    const runs = [];
    const click = singleClick(() => runs.push("toggle"), timers);
    click.onpointerdown(at(10, 10));
    click.onclick(at(10, 10));
    click.onpointerdown(at(10, 10));
    click.onclick(at(10, 10, { detail: 2 }));
    timers.run();
    expect(runs).toEqual([]);
  });
  it("does nothing after a drag or for another button", () => {
    const timers = fakeTimers();
    const runs = [];
    const click = singleClick(() => runs.push("toggle"), timers);
    click.onpointerdown(at(10, 10));
    click.onclick(at(60, 10));
    click.onpointerdown(at(10, 10, { button: 1 }));
    click.onclick(at(10, 10, { button: 1 }));
    timers.run();
    expect(runs).toEqual([]);
  });
  it("runs at once for a keyboard click", () => {
    const timers = fakeTimers();
    const runs = [];
    singleClick(() => runs.push("toggle"), timers).onclick({ button: 0, clientX: 0, clientY: 0, detail: 0 });
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

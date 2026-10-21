import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

// A fresh stack for every test: the modules (Svelte's runtime included) are
// loaded again after a reset, and OverlayHost (below) is loaded the same way,
// so all of them share one stack and one runtime.
let lib;
let svelte;

beforeEach(async () => {
  vi.resetModules();
  svelte = await import("svelte");
  lib = await import("./dialogs.js");
  const { default: Sheet } = await import("../components/sheets/ChangesSheet.svelte");
  for (const id of ["a", "b", "c"]) lib.registerDialog(id, Sheet);
});

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

const ids = () => lib.dialogs.map((entry) => entry.id);

describe("the sheet stack (lib/dialogs.js)", () => {
  it("pushes sheets in order, front last", () => {
    expect(lib.openDialog("a")).toBe(true);
    expect(lib.openDialog("b", { title: "B" })).toBe(true);
    expect(lib.openDialog("c")).toBe(true);
    expect(ids()).toEqual(["a", "b", "c"]);
    expect(lib.frontDialog()).toBe("c");
    expect(lib.dialogs[1].props).toEqual({ title: "B" });
    expect(lib.hasDialog("b")).toBe(true);
  });

  it("brings an open sheet to the front instead of opening it twice", () => {
    lib.openDialog("a");
    lib.openDialog("b");
    lib.openDialog("c");
    expect(lib.openDialog("a")).toBe(false);
    expect(ids()).toEqual(["b", "c", "a"]);
    expect(lib.bringToFront("b")).toBe(true);
    expect(ids()).toEqual(["c", "a", "b"]);
    expect(lib.bringToFront("missing")).toBe(false);
  });

  it("refuses an id nobody registered", () => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    expect(lib.openDialog("models")).toBe(false);
    expect(warn).toHaveBeenCalledOnce();
    expect(ids()).toEqual([]);
    expect(lib.frontDialog()).toBe(null);
  });

  it("closes any sheet, or the front one", () => {
    lib.openDialog("a");
    lib.openDialog("b");
    lib.openDialog("c");
    expect(lib.closeDialog("b")).toBe(true);
    expect(ids()).toEqual(["a", "c"]);
    expect(lib.closeFront()).toBe(true);
    expect(ids()).toEqual(["a"]);
    expect(lib.closeDialog("b")).toBe(false);
    lib.closeFront();
    expect(lib.closeFront()).toBe(false);
    expect(lib.hasDialog("a")).toBe(false);
  });

  it("gives a sheet opened with a grace the terminal's input grace", () => {
    vi.useFakeTimers();
    const key = { type: "keydown", key: "y" };
    lib.openDialog("a", {}, { grace: true });
    expect(lib.graceAbsorbs(key)).toBe(true);
    vi.advanceTimersByTime(425);
    expect(lib.graceAbsorbs(key)).toBe(false);
    // Reopened right after it closed: no grace (the 500 ms rule).
    lib.closeFront();
    lib.openDialog("a", {}, { grace: true });
    expect(lib.graceAbsorbs(key)).toBe(false);
    // A sheet opened without one over it ends the grace, as in the terminal.
    vi.advanceTimersByTime(600);
    lib.closeFront();
    vi.advanceTimersByTime(600);
    lib.openDialog("b", {}, { grace: true });
    expect(lib.graceAbsorbs(key)).toBe(true);
    lib.openDialog("c");
    expect(lib.graceAbsorbs(key)).toBe(false);
  });
});

describe("OverlayHost draws the stack", () => {
  let host;
  let target;

  // jsdom runs no Web Animations: with the reduce-motion setting on, every
  // open and close is instant (lib/motion.js), as in the app.
  window.matchMedia ??= (query) => ({ matches: query.includes("reduce"), media: query, addEventListener() {}, removeEventListener() {} });

  // jsdom has no `inert` property; browsers (WebKitGTK included) reflect it
  // to the attribute, which is what this shim does.
  if (!("inert" in HTMLElement.prototype)) {
    Object.defineProperty(HTMLElement.prototype, "inert", {
      configurable: true,
      get() {
        return this.hasAttribute("inert");
      },
      set(on) {
        this.toggleAttribute("inert", Boolean(on));
      },
    });
  }

  beforeEach(async () => {
    const { default: Host } = await import("../components/overlay/OverlayHost.svelte");
    target = document.createElement("div");
    document.body.appendChild(target);
    host = svelte.mount(Host, { target });
  });

  afterEach(() => {
    svelte.unmount(host);
    target.remove();
  });

  const flushSync = () => svelte.flushSync();

  const sheets = () => [...target.querySelectorAll("[data-dialog]")].map((el) => [el.dataset.dialog, el.hasAttribute("inert")]);

  it("draws nothing while the stack is empty", () => {
    flushSync();
    expect(target.querySelector(".veil")).toBe(null);
  });

  it("leaves only the front sheet without inert", () => {
    lib.openDialog("a");
    lib.openDialog("b");
    lib.openDialog("c");
    flushSync();
    expect(sheets()).toEqual([["a", true], ["b", true], ["c", false]]);
    lib.bringToFront("a");
    flushSync();
    expect(sheets()).toEqual([["b", true], ["c", true], ["a", false]]);
    lib.closeFront();
    flushSync();
    expect(sheets()).toEqual([["b", true], ["c", false]]);
    lib.closeFront();
    lib.closeFront();
    flushSync();
    expect(target.querySelector(".veil")).toBe(null);
  });

  // A {veil: false} sheet in front leaves the veil bare (no scrim); any other
  // front sheet gets the scrim, whatever lies beneath it.
  it("draws no scrim only while a {veil: false} sheet is in front", async () => {
    const { default: Sheet } = await import("../components/sheets/ChangesSheet.svelte");
    lib.registerDialog("bare", Sheet, { veil: false });
    const veil = () => {
      const el = target.querySelector(".veil");
      return [el.dataset.veil, el.classList.contains("bare")];
    };
    lib.openDialog("a");
    flushSync();
    expect(veil()).toEqual(["veil", false]);
    lib.openDialog("bare");
    flushSync();
    expect(veil()).toEqual(["none", true]);
    expect(sheets()).toEqual([["a", true], ["bare", false]]);
    lib.bringToFront("a");
    flushSync();
    expect(veil()).toEqual(["veil", false]);
  });
});

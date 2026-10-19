import { beforeEach, describe, expect, it, vi } from "vitest";

// A fresh ui store for every test.
let store;

beforeEach(async () => {
  vi.resetModules();
  store = await import("./ui.svelte.js");
});

function open(kind) {
  return store.openSlot({ kind, component: {} });
}

describe("the slot (desktop-design-5-6 7.1)", () => {
  it("takes any kind when empty, and the same kind replaces itself", () => {
    expect(open("plan")).toBe(true);
    const first = store.ui.slot;
    expect(open("plan")).toBe(true);
    expect(store.ui.slot).not.toBe(first);
  });

  it("lets permission, question and secure replace plan, customize and update", () => {
    for (const urgent of ["permission", "question", "secure"]) {
      for (const holder of ["plan", "customize", "update"]) {
        store.ui.slot = null;
        expect(open(holder)).toBe(true);
        expect(open(urgent)).toBe(true);
        expect(store.ui.slot.kind).toBe(urgent);
      }
    }
  });

  it("refuses every other kind while one holds it; update replaces nothing", () => {
    const kinds = ["permission", "question", "secure", "plan", "customize", "update"];
    const urgent = new Set(["permission", "question", "secure"]);
    for (const holder of kinds) {
      for (const kind of kinds) {
        if (kind === holder || (urgent.has(kind) && !urgent.has(holder))) continue;
        store.ui.slot = null;
        open(holder);
        expect(open(kind), `${kind} over ${holder}`).toBe(false);
        expect(store.ui.slot.kind).toBe(holder);
      }
    }
  });

  it("closes only for the kind that holds it", () => {
    open("question");
    expect(store.closeSlot("plan")).toBe(false);
    expect(store.closeSlot("question")).toBe(true);
    expect(store.ui.slot).toBe(null);
  });
});

describe("clearNotice(text) (desktop-design-5-6 7.1)", () => {
  it("clears only while that text shows; with no text it clears any notice", () => {
    store.notify("info", "Signed in", 0);
    expect(store.clearNotice("Something else")).toBe(false);
    expect(store.ui.notice?.text).toBe("Signed in");
    expect(store.clearNotice("Signed in")).toBe(true);
    expect(store.ui.notice).toBe(null);
    store.notify("info", "Newer", 0);
    expect(store.clearNotice()).toBe(true);
    expect(store.ui.notice).toBe(null);
  });
});

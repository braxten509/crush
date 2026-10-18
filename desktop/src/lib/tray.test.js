import { beforeEach, describe, expect, it, vi } from "vitest";

// A fresh tray and fresh stores for every test; the field's focus is watched.
let tray;
let sessions;
let ui;
let focusField;

beforeEach(async () => {
  vi.resetModules();
  focusField = vi.fn();
  vi.doMock("./stores/composer.svelte.js", () => ({ focusField }));
  tray = await import("./tray.js");
  ({ sessions } = await import("./stores/sessions.svelte.js"));
  ({ ui } = await import("./stores/ui.svelte.js"));
  sessions.currentId = "s1"; // the tray shows only while a chat is open
});

const key = (name, mods = {}) => ({ key: name, ctrlKey: false, altKey: false, metaKey: false, shiftKey: false, ...mods });

function section(id, order, extra = {}) {
  return { id, order, visible: () => true, header: () => id, component: null, ...extra };
}

describe("tray sections (lib/tray.js)", () => {
  it("lists the visible sections by order, whatever the order they registered in", () => {
    const shown = { queue: true };
    tray.registerTraySection(section("jobs", 40));
    tray.registerTraySection(section("queue", 20, { visible: () => shown.queue }));
    tray.registerTraySection(section("todos", 10));
    tray.registerTraySection(section("helpers", 30, { visible: () => false }));
    expect(tray.traySections.map((s) => s.id)).toEqual(["todos", "queue", "jobs"]);
    shown.queue = false;
    expect(tray.traySections.map((s) => s.id)).toEqual(["todos", "jobs"]);
    expect(tray.trayShown()).toBe(true);
  });

  it("is hidden without a chat, while the slot is held, or with nothing visible", () => {
    tray.registerTraySection(section("todos", 10));
    sessions.currentId = "";
    expect(tray.trayShown()).toBe(false);
    expect(tray.focusTray()).toBe(false);
    sessions.currentId = "s1";
    ui.slot = { kind: "permission", component: null, props: {} };
    expect(tray.trayShown()).toBe(false);
    ui.slot = null;
    expect(tray.trayShown()).toBe(true);
  });

  it("focusTray({down: true}) is false with no downFocus section", () => {
    tray.registerTraySection(section("todos", 10));
    tray.registerTraySection(section("queue", 20));
    expect(tray.focusTray({ down: true })).toBe(false);
    expect(ui.focus).toBe("composer");
    expect(tray.traySelected()).toBe("todos"); // nothing chosen yet: the first visible one
  });

  it("focusTray({down: true}) focuses the first visible downFocus section", () => {
    tray.registerTraySection(section("todos", 10));
    tray.registerTraySection(section("jobs", 40, { downFocus: true }));
    tray.registerTraySection(section("helpers", 30, { downFocus: true }));
    expect(tray.focusTray({ down: true })).toBe(true);
    expect(ui.focus).toBe("tray");
    expect(tray.traySelected()).toBe("helpers");
    // Without {down}, Tab's way in, the first visible section.
    expect(tray.focusTray()).toBe(true);
    expect(tray.traySelected()).toBe("todos");
  });

  it("moves the selection on when the selected section hides", () => {
    const shown = { queue: true };
    tray.registerTraySection(section("todos", 10));
    tray.registerTraySection(section("queue", 20, { visible: () => shown.queue, downFocus: true }));
    tray.registerTraySection(section("jobs", 40));
    tray.focusTray({ down: true });
    expect(tray.traySelected()).toBe("queue");
    shown.queue = false;
    expect(tray.traySelected()).toBe("jobs");
  });
});

describe("keys while the tray has focus", () => {
  it("lets the selected section's onKey see a key before the tray's own", () => {
    const seen = [];
    tray.registerTraySection(section("todos", 10, { onKey: (e) => (seen.push(e.key), e.key === "ArrowRight") }));
    tray.registerTraySection(section("queue", 20));
    tray.focusTray();
    // The section takes → itself, so the tray does not switch sections.
    expect(tray.trayKey(key("ArrowRight"))).toBe(true);
    expect(tray.traySelected()).toBe("todos");
    // It passes ←: the tray's own rule moves (wrapping) to the last section.
    expect(tray.trayKey(key("ArrowLeft"))).toBe(true);
    expect(tray.traySelected()).toBe("queue");
    expect(seen).toEqual(["ArrowRight", "ArrowLeft"]);
  });

  it("switches sections with ← →, wrapping, and goes back to the field with ↑, Esc and Tab", () => {
    tray.registerTraySection(section("todos", 10));
    tray.registerTraySection(section("queue", 20));
    tray.focusTray();
    expect(tray.trayKey(key("ArrowRight"))).toBe(true);
    expect(tray.traySelected()).toBe("queue");
    expect(tray.trayKey(key("ArrowRight"))).toBe(true);
    expect(tray.traySelected()).toBe("todos");
    for (const name of ["ArrowUp", "Escape", "Tab"]) expect(tray.trayKey(key(name))).toBe(true);
    expect(focusField).toHaveBeenCalledTimes(3);
    expect(tray.trayKey(key("Tab", { shiftKey: true }))).toBe(true);
    expect(ui.focus).toBe("timeline");
    // A key that types text is not taken: focus goes to the field, which types it.
    expect(tray.trayKey(key("x"))).toBe(false);
    expect(focusField).toHaveBeenCalledTimes(4);
    // Shortcuts pass through to the other scopes.
    expect(tray.trayKey(key("g", { ctrlKey: true }))).toBe(false);
    expect(focusField).toHaveBeenCalledTimes(4);
  });
});

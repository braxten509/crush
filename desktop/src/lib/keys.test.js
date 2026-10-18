import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

// A fresh key router, sheet stack and ui store for every test; the router's
// window listener is caught so tests can press keys.
let keys;
let dialogs;
let ui;
let listener;

beforeEach(async () => {
  vi.useFakeTimers();
  vi.resetModules();
  keys = await import("./keys.js");
  dialogs = await import("./dialogs.js");
  ({ ui } = await import("./stores/ui.svelte.js"));
  dialogs.registerDialog("changes", {});
  dialogs.registerDialog("quit", {});
  keys.startKeys({ addEventListener: (_type, fn) => (listener = fn) });
});

afterEach(() => {
  vi.useRealTimers();
});

// Presses a key; true when a handler took it.
function press(key, mods = {}) {
  const event = {
    key,
    code: "",
    ctrlKey: false,
    altKey: false,
    shiftKey: false,
    metaKey: false,
    isComposing: false,
    defaultPrevented: false,
    ...mods,
    preventDefault() {
      this.defaultPrevented = true;
    },
    stopPropagation() {},
  };
  listener(event);
  return event.defaultPrevented;
}

describe("keys while the menu, the viewer or a sheet is open (desktop-design 1.8)", () => {
  it("asks no global key but the quit key, as the terminal's front dialog takes every key", () => {
    const help = vi.fn(() => true);
    keys.bindKey("global", "Ctrl+G", help);
    expect(press("g", { ctrlKey: true })).toBe(true);
    expect(help).toHaveBeenCalledTimes(1);

    dialogs.openDialog("changes");
    expect(keys.activeScopes("Ctrl+G")).toEqual(["overlay"]);
    expect(press("g", { ctrlKey: true })).toBe(false);
    dialogs.closeDialog("changes");

    ui.menu = { items: [] };
    expect(keys.activeScopes("Ctrl+G")).toEqual(["menu"]);
    expect(press("g", { ctrlKey: true })).toBe(false);
    ui.menu = null;

    ui.viewer = { path: "a.png" };
    expect(press("g", { ctrlKey: true })).toBe(false);
    ui.viewer = null;
    expect(help).toHaveBeenCalledTimes(1);
  });

  it("asks global first for Ctrl+C over any of them, except over the quit sheet", () => {
    const quit = vi.fn(() => true);
    const sheet = vi.fn(() => true);
    keys.bindKey("global", "Ctrl+C", quit);
    keys.bindKey("overlay", "Ctrl+C", sheet);

    dialogs.openDialog("changes");
    expect(keys.activeScopes("Ctrl+C")).toEqual(["global", "overlay"]);
    expect(press("c", { ctrlKey: true })).toBe(true);
    expect([quit.mock.calls.length, sheet.mock.calls.length]).toEqual([1, 0]);

    dialogs.openDialog("quit");
    expect(keys.activeScopes("Ctrl+C")).toEqual(["overlay"]);
    expect(press("c", { ctrlKey: true })).toBe(true);
    expect([quit.mock.calls.length, sheet.mock.calls.length]).toEqual([1, 1]);
  });

  it("lets Ctrl+C past a sheet's input grace while other keys are absorbed", () => {
    const quit = vi.fn(() => true);
    const typed = vi.fn(() => true);
    keys.bindKey("global", "Ctrl+C", quit);
    keys.bindKey("overlay", "x", typed);
    dialogs.openDialog("changes", {}, { grace: true });
    expect(press("x")).toBe(true); // absorbed by the grace
    expect(typed).not.toHaveBeenCalled();
    expect(press("c", { ctrlKey: true })).toBe(true);
    expect(quit).toHaveBeenCalledTimes(1);
  });

  it("without them asks overlay, then the focused region, then global; Ctrl+C asks global first", () => {
    ui.focus = "composer";
    expect(keys.activeScopes("Ctrl+N")).toEqual(["overlay", "popup", "composer", "global"]);
    expect(keys.activeScopes("Ctrl+C")).toEqual(["global", "overlay", "popup", "composer"]);
    ui.focus = "timeline";
    expect(keys.activeScopes("Ctrl+N")).toEqual(["overlay", "timeline", "global"]);
  });
});

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

// u:kit.grace_repeat (desktop-design-5-6 2.1, DLG-005): S3's key router, its
// sheet stack and grace, and KIT's overlay "*" handler bound as
// plugins/kit.js binds it. A sheet that closed (by Esc through the kit, or by
// its own button) and opens again with {grace: true} within 500 ms takes the
// very next key; after 500 ms, or for another sheet, the grace absorbs keys
// again (internal/ui/dialog/dialog.go:125-137, :173-188).
let keys;
let dialogs;
let kit;
let listener;
let heard;

beforeEach(async () => {
  vi.useFakeTimers();
  vi.setSystemTime(new Date("2026-10-19T12:00:00Z"));
  vi.resetModules();
  keys = await import("../keys.js");
  dialogs = await import("../dialogs.js");
  kit = await import("./sheetkeys.js");
  keys.bindKey("overlay", "*", kit.sheetKey);
  keys.startKeys({ addEventListener: (_type, fn) => (listener = fn) });
  heard = [];
  // Two sheets that note every key their onKey hears and take the letters.
  for (const id of ["permissions", "aws_sso"]) {
    dialogs.registerDialog(id, {});
    kit.setSheet(id, {
      onKey: () => (event) => {
        heard.push(`${id}:${event.key}`);
        return event.key.length === 1;
      },
      root: () => null,
      report: () => ({}),
    });
  }
});

afterEach(() => {
  vi.useRealTimers();
});

// Presses a key; true when something took it (a sheet, the kit or the grace).
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

describe("a sheet's input grace when it opens again (DLG-005)", () => {
  it("grace_repeat", () => {
    // Opened with a grace: the first key is absorbed, not heard.
    dialogs.openDialog("permissions", {}, { grace: true });
    expect(press("y")).toBe(true);
    expect(heard).toEqual([]);
    vi.advanceTimersByTime(500); // quiet: the sheet takes keys
    press("y");
    expect(heard).toEqual(["permissions:y"]);

    // Esc reaches the sheet, which leaves it, so the kit closes it.
    expect(press("Escape")).toBe(true);
    expect(dialogs.hasDialog("permissions")).toBe(false);
    expect(heard.at(-1)).toBe("permissions:Escape");

    // Back within 500 ms: no grace, the next key is the sheet's.
    vi.advanceTimersByTime(300);
    dialogs.openDialog("permissions", {}, { grace: true });
    press("n");
    expect(heard.at(-1)).toBe("permissions:n");

    // Alt+Esc closes too; back after 500 ms: the grace again.
    expect(press("Escape", { altKey: true })).toBe(true);
    expect(dialogs.hasDialog("permissions")).toBe(false);
    vi.advanceTimersByTime(500);
    dialogs.openDialog("permissions", {}, { grace: true });
    const before = heard.length;
    expect(press("n")).toBe(true);
    expect(heard.length).toBe(before);

    // Closed by its own button (closeDialog) counts as a close as well.
    vi.advanceTimersByTime(500);
    dialogs.closeDialog("permissions");
    vi.advanceTimersByTime(100);
    dialogs.openDialog("permissions", {}, { grace: true });
    press("a");
    expect(heard.at(-1)).toBe("permissions:a");

    // Another sheet right after it keeps its protection.
    press("Escape");
    vi.advanceTimersByTime(50);
    dialogs.openDialog("aws_sso", {}, { grace: true });
    const count = heard.length;
    expect(press("x")).toBe(true);
    expect(heard.length).toBe(count);
    expect(dialogs.frontDialog()).toBe("aws_sso");
  });
});

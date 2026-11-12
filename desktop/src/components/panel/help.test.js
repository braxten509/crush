import { describe, expect, it } from "vitest";
import { helpGroups } from "./help.js";

// SIDE-029, SIDE-035, TYPE-039: the full help follows the state, as
// internal/ui/model/testdata/TestHelpBindings.golden does. These check which
// keys are offered, not the words or the order: a key that does nothing in a
// state must not be offered there.

const keys = (state) => helpGroups(state).flatMap((g) => g.entries.map((x) => x.key));
const groupKeys = (state, id) => (helpGroups(state).find((g) => g.id === id)?.entries ?? []).map((x) => x.key);

describe("helpGroups", () => {
  // [state name, state, key, offered?]
  const TABLE = [
    ["landing", { screen: "landing", focus: "composer" }, "tab", false],
    ["landing", { screen: "landing", focus: "composer" }, "shift+tab", true],
    ["chat, panel focused, composer_focus_only", { screen: "chat", focus: "panel", hasSession: true, typed: true, focusOnly: true }, "tab", false],
    ["the chat list", { screen: "chat", focus: "list", hasSession: true }, "tab", false],
    ["the chat list", { screen: "chat", focus: "list", hasSession: true }, "↑↓", false],
    ["the chat list", { screen: "chat", focus: "list", hasSession: true }, "g", false],
    ["the chat list", { screen: "chat", focus: "list", hasSession: true }, "shift+enter", false],
    ["chat, timeline focused", { screen: "chat", focus: "timeline", hasSession: true }, "tab", true],
    ["chat, timeline focused", { screen: "chat", focus: "timeline", hasSession: true }, "↑↓", true],
  ];

  it.each(TABLE)("%s: offers %s? (%#)", (_, state, key, offered) => {
    expect(keys(state).includes(key)).toBe(offered);
  });

  it("landing: the full help's rows only, no Tab (golden 801-805; SIDE-035)", () => {
    const got = keys({ screen: "landing", focus: "composer" });
    expect(got).not.toContain("tab");
    expect(got).toContain("/ or ctrl+p");
    // With text in the field, "/" no longer opens the commands.
    const typed = keys({ screen: "landing", focus: "composer", typed: true });
    expect(typed).toContain("ctrl+p");
    expect(typed).not.toContain("/ or ctrl+p");
  });

  it("chat, field focused: esc cancel while working with no queue (golden 1630-1636); nothing while idle (1624-1629)", () => {
    const working = keys({ screen: "chat", focus: "composer", busy: true, hasSession: true });
    expect(working).toContain("esc");
    expect(working).not.toContain("ctrl+enter");
    const queued = keys({ screen: "chat", focus: "composer", busy: true, queued: 2, hasSession: true });
    expect(queued).toContain("esc");
    expect(queued).toContain("ctrl+enter");
    const idle = keys({ screen: "chat", focus: "composer", hasSession: true });
    expect(idle).not.toContain("esc");
    expect(idle).not.toContain("ctrl+enter");
    expect(idle).toContain("tab");
  });

  it("the attachment keys follow the chips (TYPE-039); the picture keys follow the model", () => {
    const base = { screen: "chat", focus: "composer", hasSession: true };
    expect(keys({ ...base, chips: true })).toEqual(expect.arrayContaining(["ctrl+r+{i}", "ctrl+r+r"]));
    expect(keys(base).some((key) => key.startsWith("ctrl+r"))).toBe(false);
    expect(groupKeys({ ...base, images: true }, "field")).toEqual(expect.arrayContaining(["ctrl+f", "ctrl+v"]));
    expect(groupKeys(base, "field")).not.toContain("ctrl+f");
  });

  it("a command that can move to the background adds ctrl+b to the chat row", () => {
    const base = { screen: "chat", focus: "timeline", hasSession: true };
    expect(groupKeys({ ...base, backgroundable: true }, "chat")).toContain("ctrl+b");
    expect(keys(base)).not.toContain("ctrl+b");
  });

  it("the tray's section keys show while it has focus, with both sections", () => {
    expect(keys({ screen: "chat", focus: "tray", hasSession: true, pills: true })).toContain("←/→");
    expect(keys({ screen: "chat", focus: "tray", hasSession: true })).not.toContain("←/→");
    expect(keys({ screen: "chat", focus: "tray", hasSession: true })).toContain("tab");
  });

  it("a slot form: its own help while focused; Tab back to it and the scroll keys otherwise (help.go:143-153)", () => {
    const slot = { entries: [{ key: "space", action: "toggle" }, { key: "enter", action: "done" }] };
    const focused = keys({ screen: "chat", focus: "slot", hasSession: true, slot });
    expect(focused).toEqual(expect.arrayContaining(["space", "enter"]));
    expect(focused).not.toContain("tab");
    expect(focused).not.toContain("↑↓");
    const away = keys({ screen: "chat", focus: "timeline", hasSession: true, slot });
    expect(away).toEqual(expect.arrayContaining(["tab", "↑↓"]));
    expect(away).not.toContain("space");
    const focusOnly = keys({ screen: "chat", focus: "timeline", hasSession: true, slot, focusOnly: true });
    expect(focusOnly).toContain("click");
    expect(focusOnly).not.toContain("tab");
    // A form without help words shows nothing (golden 1651-1653).
    expect(keys({ screen: "chat", focus: "slot", hasSession: true, slot: {} })).toEqual([]);
  });
});

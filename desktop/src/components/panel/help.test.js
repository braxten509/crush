import { describe, expect, it } from "vitest";
import { helpGroups, helpLines } from "./help.js";

// SIDE-029, SIDE-035, TYPE-039: the full help of
// internal/ui/model/testdata/TestHelpBindings.golden, state by state (the
// golden's line numbers are given). Desktop differences, each on purpose:
// the desktop always tells keys apart ("ctrl+m models", "shift+enter
// newline", update_ui.go:86-91); Ctrl+End gets lib/keys.js's words (the
// terminal lists it with none, "="); a disabled Tab ("-") is left out; the
// to-do section keys show while the tray has focus (SIDE-033).

const EDITOR = [
  "shift+enter newline",
  "@ mention file",
  "ctrl+o open editor",
  "ctrl+shift+v paste text",
  "ctrl+shift+a select all",
  "ctrl+shift+c copy selection",
  "ctrl+shift+x cut selection",
];
const ATTACHMENTS = ["ctrl+r+{i} delete attachment at index i", "ctrl+r+r delete all attachments", "esc cancel delete mode"];
const LAST = ["ctrl+g less", "ctrl+c quit"];
const END = "ctrl+end jump to the bottom and follow";
const lines = (state) => helpLines(helpGroups(state));

describe("helpGroups", () => {
  it("init: quit (golden 1-4)", () => {
    expect(lines({ screen: "init", focus: "composer" })).toEqual(["ctrl+c quit", ...LAST]);
    expect(lines({ screen: "init", focus: "timeline", busy: true, queued: 2, hasSession: true })).toEqual(["ctrl+c quit", ...LAST]);
  });

  it("landing: the full help's rows only, no Tab (golden 801-805; SIDE-035)", () => {
    const got = lines({ screen: "landing", focus: "composer" });
    expect(got).toEqual(["shift+tab mode", "/ or ctrl+p commands", "ctrl+m models", "ctrl+s sessions", "ctrl+y toggle yolo", ...EDITOR, ...LAST]);
    expect(got.some((line) => line.startsWith("tab "))).toBe(false);
    // With text in the field, "/" no longer opens the commands.
    expect(lines({ screen: "landing", focus: "composer", typed: true })[1]).toBe("ctrl+p commands");
  });

  it("landing with a session: only the last row (golden 821-823)", () => {
    expect(lines({ screen: "landing", focus: "composer", hasSession: true })).toEqual(LAST);
  });

  it("chat, field focused, working with prompts queued (golden 1643-1650)", () => {
    expect(lines({ screen: "chat", focus: "composer", busy: true, queued: 2, hasSession: true, chips: true })).toEqual([
      "esc interrupt, send queued",
      "ctrl+enter interrupt, send queued",
      "tab focus chat",
      "shift+tab mode",
      "/ or ctrl+p commands",
      "ctrl+m models",
      "ctrl+s sessions",
      "ctrl+y toggle yolo",
      "ctrl+n new session",
      END,
      ...EDITOR,
      ...ATTACHMENTS,
      ...LAST,
    ]);
  });

  it("chat, field focused: esc cancel while working with no queue (golden 1630-1636); nothing while idle (1624-1629)", () => {
    expect(lines({ screen: "chat", focus: "composer", busy: true, hasSession: true })[0]).toBe("esc cancel");
    expect(lines({ screen: "chat", focus: "composer", hasSession: true })[0]).toBe("tab focus chat");
  });

  it("chat without a session, typed (golden 1675-1679)", () => {
    expect(lines({ screen: "chat", focus: "composer", typed: true })).toEqual([
      "tab focus chat",
      "shift+tab mode",
      "ctrl+p commands",
      "ctrl+m models",
      "ctrl+s sessions",
      "ctrl+y toggle yolo",
      ...EDITOR,
      ...LAST,
    ]);
  });

  it("chat, timeline focused (golden 1951-1960)", () => {
    expect(lines({ screen: "chat", focus: "timeline", busy: true, queued: 2, hasSession: true, chips: true, pills: true })).toEqual([
      "esc interrupt, send queued",
      "ctrl+enter interrupt, send queued",
      "tab focus editor",
      "shift+tab mode",
      "ctrl+p commands",
      "ctrl+m models",
      "ctrl+s sessions",
      "ctrl+y toggle yolo",
      "ctrl+n new session",
      END,
      "↑↓ scroll",
      "shift+↑↓ scroll one item",
      "b/pgup page up",
      "f/pgdn page down",
      "u half page up",
      "d half page down",
      "g home",
      "G end",
      END,
      "l/→ focus sidebar",
      "c/y copy",
      "esc clear selection",
      ...LAST,
    ]);
  });

  it("chat, panel focused, with composer_focus_only: no Tab (golden 2650-2656)", () => {
    expect(lines({ screen: "chat", focus: "panel", hasSession: true, typed: true, focusOnly: true })).toEqual([
      "shift+tab mode",
      "ctrl+p commands",
      "ctrl+m models",
      "ctrl+s sessions",
      "ctrl+y toggle yolo",
      "ctrl+n new session",
      END,
      "↑↓ scroll",
      "h/← focus chat",
      "g home",
      "G end",
      ...LAST,
    ]);
  });

  it("the attachment keys follow the chips (TYPE-039); the picture keys follow the model", () => {
    const base = { screen: "chat", focus: "composer", hasSession: true };
    expect(lines({ ...base, chips: true })).toEqual(expect.arrayContaining(ATTACHMENTS));
    expect(lines(base).some((line) => line.startsWith("ctrl+r"))).toBe(false);
    const field = helpGroups({ ...base, images: true }).find((g) => g.id === "field");
    expect(helpLines([field]).slice(-2)).toEqual(["ctrl+f add image", "ctrl+v paste image from clipboard"]);
  });

  it("a command that can move to the background adds ctrl+b to the chat row", () => {
    const chat = helpGroups({ screen: "chat", focus: "timeline", hasSession: true, backgroundable: true }).find((g) => g.id === "chat");
    expect(helpLines([chat]).at(-1)).toBe("ctrl+b background command");
  });

  it("the tray's section keys show while it has focus, with both sections", () => {
    expect(lines({ screen: "chat", focus: "tray", hasSession: true, pills: true })).toContain("←/→ switch section");
    expect(lines({ screen: "chat", focus: "tray", hasSession: true })).not.toContain("←/→ switch section");
    expect(lines({ screen: "chat", focus: "tray", hasSession: true })).toContain("tab focus editor");
  });

  it("the chat list: no Tab entry and no pane rows", () => {
    expect(lines({ screen: "chat", focus: "list", hasSession: true })).toEqual([
      "shift+tab mode",
      "ctrl+p commands",
      "ctrl+m models",
      "ctrl+s sessions",
      "ctrl+y toggle yolo",
      "ctrl+n new session",
      END,
      ...LAST,
    ]);
  });

  it("a slot form: its own help while focused; Tab back to it and the scroll keys otherwise (help.go:143-153)", () => {
    const slot = { entries: [{ key: "space", action: "toggle" }, { key: "enter", action: "done" }] };
    expect(lines({ screen: "chat", focus: "slot", hasSession: true, slot })).toEqual(["space toggle", "enter done"]);
    const away = ["↑↓ scroll", "shift+↑↓ scroll one item", "b/pgup page up", "f/pgdn page down", "u half page up", "d half page down", "g home", "G end"];
    expect(lines({ screen: "chat", focus: "timeline", hasSession: true, slot })).toEqual(["tab focus editor", ...away]);
    expect(lines({ screen: "chat", focus: "timeline", hasSession: true, slot, focusOnly: true })[0]).toBe("click focus editor");
    expect(lines({ screen: "chat", focus: "timeline", hasSession: true, slot: { ...slot, collapsedHelp: "open plan" } })[0]).toBe("tab open plan");
    // A form without help words shows nothing (golden 1651-1653).
    expect(lines({ screen: "chat", focus: "slot", hasSession: true, slot: {} })).toEqual([]);
  });

  it("groups carry the desktop's small-caps labels", () => {
    const groups = helpGroups({ screen: "chat", focus: "composer", busy: true, hasSession: true, chips: true });
    expect(groups.map((g) => g.label)).toEqual(["RUNNING", "CHAT", "FIELD", "ATTACHMENTS", "HELP"]);
  });
});

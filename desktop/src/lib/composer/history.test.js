import { describe, expect, it } from "vitest";
import {
  PromptHistory,
  atEnd,
  atStart,
  bangAfterPaste,
  bangAfterTyping,
  bangFromValue,
  historyEntries,
  joinDrafts,
  onFirstLine,
  onLastLine,
} from "./history.js";

const user = (...parts) => ({ role: "user", parts });
const text = (value, extra = {}) => ({ type: "text", data: { text: value, ...extra } });
const shell = (command) => ({ type: "shell_command", data: { command, output: "", exit_code: 0 } });

describe("historyEntries (TYPE-017, history.go:22-61)", () => {
  it("keeps the engine's newest-first order and gives text, then each shell command", () => {
    const list = [user(text("third")), user(text(""), shell("ls -la")), user(text("first"), shell("pwd"))];
    expect(historyEntries(list)).toEqual(["third", "!ls -la", "first", "!pwd"]);
  });

  it("skips hidden prompts and task results", () => {
    const list = [
      user(text("Implement the plan.", { hidden: true })),
      user(text("  <crush-task-result><name>t1</name></crush-task-result>")),
      user(text("kept")),
    ];
    expect(historyEntries(list)).toEqual(["kept"]);
  });

  it("is empty for nothing", () => {
    expect(historyEntries(null)).toEqual([]);
  });
});

describe("PromptHistory (TYPE-017, history.go:183-282)", () => {
  const history = () => {
    const h = new PromptHistory();
    h.load(["three", "two", "one"]);
    return h;
  };

  it("goes older from the draft, keeping the draft, then newer back to it", () => {
    const h = history();
    expect(h.prev("my draft")).toBe("three");
    expect(h.prev("three")).toBe("two");
    expect(h.next()).toBe("three");
    expect(h.next()).toBe("my draft");
    expect(h.browsing()).toBe(false);
    expect(h.next()).toBeNull();
  });

  it("stops at the oldest entry", () => {
    const h = history();
    h.prev("");
    h.prev("");
    expect(h.prev("")).toBe("one");
    expect(h.prev("one")).toBeNull();
    expect(h.index).toBe(2);
  });

  it("Esc gives the draft back only while browsing", () => {
    const h = history();
    expect(h.escape()).toBeNull();
    h.prev("half written");
    h.prev("x");
    expect(h.escape()).toBe("half written");
    expect(h.browsing()).toBe(false);
  });

  it("an edit makes the text the draft; a send forgets it", () => {
    const h = history();
    h.prev("draft");
    h.edited("three, edited");
    expect(h.browsing()).toBe(false);
    expect(h.draft).toBe("three, edited");
    h.reset();
    expect(h.draft).toBe("");
    expect(h.prev("")).toBe("three");
  });

  it("does nothing without entries", () => {
    const h = new PromptHistory();
    expect(h.prev("draft")).toBeNull();
    expect(h.browsing()).toBe(false);
  });
});

describe("caret places (composer.go:51-66)", () => {
  it("knows the start, the end and the first and last lines", () => {
    expect(atStart("ab\ncd", 0)).toBe(true);
    expect(atStart("ab\ncd", 1)).toBe(false);
    expect(atEnd("ab\ncd", 5)).toBe(true);
    expect(atEnd("ab\ncd", 4)).toBe(false);
    expect(onFirstLine("ab\ncd", 2)).toBe(true);
    expect(onFirstLine("ab\ncd", 3)).toBe(false);
    expect(onLastLine("ab\ncd", 3)).toBe(true);
    expect(onLastLine("ab\ncd", 2)).toBe(false);
  });
});

describe("bang mode", () => {
  it("an entry starting with ! switches to bang mode without the ! (TYPE-018)", () => {
    expect(bangFromValue("!ls -la", false)).toEqual({ bang: true, value: "ls -la", wasEmpty: false });
    expect(bangFromValue("plain", true)).toEqual({ bang: false, value: "plain", wasEmpty: false });
    expect(bangFromValue("plain", false)).toEqual({ bang: false, value: "plain", wasEmpty: null });
  });

  it("! typed at the start enters, spaces before it allowed (TYPE-020, keypress.go:428-449)", () => {
    expect(bangAfterTyping({ before: "", value: "!", caret: 1, bang: false, wasEmpty: false })).toEqual({
      bang: true,
      wasEmpty: true,
      value: "",
      caret: 0,
    });
    expect(bangAfterTyping({ before: "  ", value: "  !", caret: 3, bang: false, wasEmpty: false })).toMatchObject({
      bang: true,
      value: "",
      caret: 0,
    });
    expect(bangAfterTyping({ before: "ls", value: "!ls", caret: 1, bang: false, wasEmpty: false })).toEqual({
      bang: true,
      wasEmpty: false,
      value: "ls",
      caret: 0,
    });
  });

  it("! later in the text stays text", () => {
    expect(bangAfterTyping({ before: "ab", value: "ab!", caret: 3, bang: false, wasEmpty: false }).bang).toBe(false);
  });

  it("emptying the field in bang mode arms the leaving Backspace", () => {
    expect(bangAfterTyping({ before: "l", value: "", caret: 0, bang: true, wasEmpty: false }).wasEmpty).toBe(true);
    expect(bangAfterTyping({ before: "", value: "l", caret: 1, bang: true, wasEmpty: true }).wasEmpty).toBe(false);
  });

  it("a paste starting with ! enters bang mode (TYPE-013, paste.go:36-53)", () => {
    expect(bangAfterPaste({ value: "  !echo hi", caret: 10, bang: false })).toEqual({ bang: true, wasEmpty: true, value: "echo hi", caret: 7 });
    expect(bangAfterPaste({ value: "say !hi", caret: 7, bang: false })).toBeNull();
    expect(bangAfterPaste({ value: "!x", caret: 2, bang: true })).toBeNull();
  });
});

describe("joinDrafts (TYPE-019, history.go:155-181)", () => {
  it("puts a blank line between non-empty parts", () => {
    expect(joinDrafts("", [{ text: "first" }, { text: "second" }])).toBe("first\n\nsecond");
    expect(joinDrafts("typed", [{ text: "" }, { text: "queued" }])).toBe("typed\n\nqueued");
    expect(joinDrafts("typed", [])).toBe("typed");
  });
});

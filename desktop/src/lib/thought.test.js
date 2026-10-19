// CHAT-022: the thought peek (thought_line.go:30-55 and the cases of
// thought_line_test.go).
import { describe, expect, test } from "vitest";
import { cleanThoughtLine, isThinking, reasoningOf, thoughtLineEnabled, thoughtLine } from "./thought.js";

const reasoning = (thinking, extra = {}) => ({ type: "reasoning", data: { thinking, signature: "", ...extra } });
const text = (t) => ({ type: "text", data: { text: t } });
const finish = (reason = "end_turn") => ({ type: "finish", data: { reason, time: 1 } });
const msg = (parts, extra = {}) => ({ id: "a", role: "assistant", parts, ...extra });

describe("thought line", () => {
  test("nothing before any thinking", () => {
    expect(thoughtLine(msg([]))).toBe("");
  });

  test("the newest line of the streamed thinking, marks removed", () => {
    expect(thoughtLine(msg([reasoning("First step.\n\n## Second step\n", { started_at: 5 })]))).toBe("Second step");
    expect(thoughtLine(msg([reasoning("Look at the result.\nPick the next file to read.")]))).toBe("Pick the next file to read.");
  });

  test("a CLI's held line (activity_detail) wins, with its Markdown marks removed", () => {
    const m = msg([reasoning("First step.")], { activity: "thinking", activity_detail: "**Checking how Magisk asks for su approval**" });
    expect(thoughtLine(m)).toBe("Checking how Magisk asks for su approval");
    expect(thoughtLine(msg([], { activity_detail: "Held line." }))).toBe("Held line.");
  });

  test("empty once the thinking ended or the message finished", () => {
    expect(thoughtLine(msg([reasoning("Done thinking", { started_at: 1, finished_at: 2 })]))).toBe("");
    expect(thoughtLine(msg([reasoning("Still?"), finish()]))).toBe("");
    expect(thoughtLine(msg([reasoning("Hidden"), text("Reply begins")]))).toBe("");
  });

  test("off with the setting", () => {
    const m = msg([reasoning("Thinking hard")]);
    expect(thoughtLine(m, { enabled: false })).toBe("");
    expect(thoughtLineEnabled({ options: { tui: { thought_line: false } } })).toBe(false);
    expect(thoughtLineEnabled({ options: { tui: {} } })).toBe(true);
    expect(thoughtLineEnabled(null)).toBe(true);
  });

  test("cleaning keeps the last non-blank line without escapes", () => {
    expect(cleanThoughtLine("a\n\x1b[31m**b**\x1b[0m\n\n  ")).toBe("b");
    expect(cleanThoughtLine("`code`_")).toBe("code");
    expect(cleanThoughtLine("# ")).toBe("");
  });
});

describe("reasoning helpers (content.go:220-240, 339-342)", () => {
  test("blocks join with a blank line; the finish time counts only when all finished", () => {
    const m = msg([reasoning("one", { started_at: 3, finished_at: 4 }), reasoning("two", { finished_at: 0 })]);
    expect(reasoningOf(m)).toEqual({ thinking: "one\n\ntwo", started_at: 3, finished_at: 0 });
    expect(isThinking(m)).toBe(true);
    const done = msg([reasoning("one", { started_at: 3, finished_at: 4 }), reasoning("two", { finished_at: 9 })]);
    expect(reasoningOf(done).finished_at).toBe(9);
    expect(isThinking(done)).toBe(false);
  });

  test("a started thinking with no text yet counts", () => {
    expect(isThinking(msg([reasoning("", { started_at: 7 })]))).toBe(true);
    expect(isThinking(msg([]))).toBe(false);
  });
});

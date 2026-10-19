// CHAT-052, CHAT-057 and CHAT-113: tool bodies start at 10 lines plus the
// hidden count, and never hide a single line (tools.go collapsedMaxLines,
// tool_output.go).
import { describe, expect, it } from "vitest";
import { codeLines, collapse, collapsedMaxLines, diffShown, hiddenText, markdownLines, MAX_LINES, plainLines } from "./body.js";

const numbered = (n) => Array.from({ length: n }, (_, i) => `line ${i + 1}`).join("\n");

describe("collapsing (CHAT-052)", () => {
  it("shows 10 lines whole, 11 lines whole, and 10 of 12 with the hidden count", () => {
    expect(MAX_LINES).toBe(10);
    expect(collapsedMaxLines(10)).toBe(10);
    expect(collapsedMaxLines(11)).toBe(11);
    expect(collapsedMaxLines(12)).toBe(10);
    expect(plainLines(numbered(10), false)).toEqual({ lines: numbered(10).split("\n"), hidden: 0 });
    expect(plainLines(numbered(11), false).hidden).toBe(0);
    const twelve = plainLines(numbered(12), false);
    expect(twelve.lines).toHaveLength(10);
    expect(twelve.lines.at(-1)).toBe("line 10");
    expect(twelve.hidden).toBe(2);
    expect(hiddenText(2)).toBe("… (2 lines hidden) [click or space to expand]");
  });
  it("shows everything once opened", () => {
    expect(collapse(25, true)).toEqual({ shown: 25, hidden: 0 });
    expect(plainLines(numbered(25), true).lines).toHaveLength(25);
  });
  it("normalizes the text first: CRLF, tabs, outer newlines, cursor moves and \\r redraws", () => {
    expect(plainLines("\n\na\r\n\tb\n\n", false).lines).toEqual(["a", "    b"]);
    expect(plainLines("10%\r50%\r100%\nok\x1b[2K", false).lines).toEqual(["100%", "ok"]);
    expect(plainLines("\x1b[31mred\x1b[0m", false).lines).toEqual(["\x1b[31mred\x1b[0m"]);
  });
});

describe("code bodies (CHAT-057)", () => {
  it("numbers from the offset, with the gutter as wide as the last shown number", () => {
    expect(codeLines(numbered(3), 0, false)).toMatchObject({ first: 1, digits: 1, hidden: 0 });
    expect(codeLines(numbered(12), 95, false)).toMatchObject({ first: 96, digits: 3, hidden: 2 });
    expect(codeLines(numbered(12), 95, false).lines).toHaveLength(10);
    // 10 shown lines from 0 end at 10: two digits, as getDigits(10).
    expect(codeLines(numbered(30), 0, false).digits).toBe(2);
    expect(codeLines(numbered(9), 0, false).digits).toBe(1);
    expect(codeLines("x", undefined, false).first).toBe(1);
  });
});

describe("Markdown and diff bodies", () => {
  it("cuts Markdown by its own lines", () => {
    expect(markdownLines(numbered(12), false)).toEqual({ text: numbered(10), hidden: 2 });
    expect(markdownLines(numbered(11), false).hidden).toBe(0);
  });
  it("cuts a diff's rows the same way; 0 shows all (CHAT-113)", () => {
    expect(diffShown(12, 10)).toEqual({ shown: 10, hidden: 2 });
    expect(diffShown(11, 10)).toEqual({ shown: 11, hidden: 0 });
    expect(diffShown(40, 0)).toEqual({ shown: 40, hidden: 0 });
  });
});

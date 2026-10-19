// DLG-294 to DLG-311: the Changes sheet's rules (ports of dialog/changes.go,
// its changes_test.go, and model/reviews.go with reviews_test.go).
import { describe, expect, it } from "vitest";
import { cells } from "../diff.js";
import { ADDED, buildReview, DELETED, MODIFIED } from "./review.js";
import {
  callsFromChat,
  codeText,
  countNoun,
  EXPIRED_TEXT,
  fileHead,
  FOOTER_KEYS,
  footerKeys,
  isExpired,
  jumpTarget,
  kindTone,
  listStat,
  loadSheetFiles,
  numberDigits,
  SHEET_KEYS,
  shortPath,
  sourceLine,
  startsFolded,
  truncateLeft,
} from "./sheet.js";

// changes_test.go:17-31.
function fixture() {
  let before = "";
  let after = "";
  for (let i = 1; i <= 80; i++) {
    before += `line ${i}\n`;
    after += i === 5 || i === 70 ? `changed ${i}\n` : `line ${i}\n`;
  }
  return buildReview([
    { path: "/p/a.go", before, after, full: true },
    { path: "/p/new.txt", before: "", after: "hello " + "word ".repeat(40) + "\n", full: true },
  ]);
}

describe("heading and file list (DLG-295, DLG-296)", () => {
  it("says the source, the file count and the totals", () => {
    expect(sourceLine("Ran 3 tools", fixture())).toEqual({ text: "Ran 3 tools · 2 files", adds: 3, dels: 2 });
    const removed = buildReview([{ path: "x", snapshot: { path: "x", before: { content: "a\n" } } }]);
    expect(sourceLine("Ran 1 tool", removed)).toEqual({ text: "Ran 1 tool · 1 file", adds: null, dels: null });
    expect(countNoun(0, "file")).toBe("0 files");
  });
  it("colors kinds: added, removed, other", () => {
    expect(kindTone(ADDED)).toBe("add");
    expect(kindTone(DELETED)).toBe("del");
    expect(kindTone(MODIFIED)).toBe("other");
  });
  it("shows counts, or the kind when a file has none", () => {
    const [a, n] = fixture();
    expect(listStat(a)).toEqual({ adds: 2, dels: 2 });
    expect(listStat(n)).toEqual({ adds: 1, dels: 0 });
    const [gone] = buildReview([{ path: "x", snapshot: { path: "x", before: { content: "a\n" } } }]);
    expect(listStat(gone)).toEqual({ label: "Deleted" });
    const [binary] = buildReview([{ path: "b", snapshot: { path: "b", before: { omitted: "Binary file", size: 1 }, after: { omitted: "Binary file", size: 2 } } }]);
    expect(listStat(binary)).toEqual({ label: "Changed" });
  });
});

describe("file headers (DLG-297)", () => {
  it("shows the kind badge and the counts when counted", () => {
    const [a, n] = fixture();
    expect(fileHead(a)).toEqual({ badge: "Changed", tone: "other", counts: { adds: 2, dels: 2 } });
    expect(fileHead(n)).toEqual({ badge: "Added", tone: "add", counts: { adds: 1, dels: 0 } });
    const [gone] = buildReview([{ path: "x", snapshot: { path: "x", before: { content: "a\n" } } }]);
    expect(fileHead(gone)).toEqual({ badge: "Deleted", tone: "del", counts: null });
  });
  it("gives the number columns at least 3 digits", () => {
    expect(numberDigits(fixture()[0])).toBe(3);
    expect(numberDigits({ lines: [{ old: 12345, new: 0 }] })).toBe(5);
  });
});

describe("folding (DLG-298)", () => {
  it("folds a diff over 600 lines", () => {
    expect(startsFolded({ lines: new Array(600) })).toBe(false);
    expect(startsFolded({ lines: new Array(601) })).toBe(true);
  });
});

describe("code and paths (DLG-300)", () => {
  it("expands tabs to 4-column stops and drops control characters", () => {
    expect(codeText("\tx")).toBe("    x");
    expect(codeText("ab\tc")).toBe("ab  c");
    expect(codeText("abcd\te")).toBe("abcd    e");
    expect(codeText("a\x00b\x1bc\x7fd\re")).toBe("abcde");
  });
  it("shows paths relative to the project folder", () => {
    expect(shortPath("/w/src/a.go", "/w")).toBe("src/a.go");
    expect(shortPath("/w", "/w")).toBe(".");
    expect(shortPath("/other/a.go", "/w")).toBe("/other/a.go");
    expect(shortPath("/w2/a.go", "/w")).toBe("/w2/a.go");
    expect(shortPath("rel/a.go", "")).toBe("rel/a.go");
  });
  it("cuts a long path from the left, keeping the file name", () => {
    // 29 cells into 12: "…" and the last 11.
    expect(truncateLeft("internal/ui/chat/toolgroup.go", 12)).toBe("…oolgroup.go");
    expect(truncateLeft("internal/ui/chat/toolgroup.go", 13)).toBe("…toolgroup.go");
    expect(cells(truncateLeft("internal/ui/chat/toolgroup.go", 12))).toBe(12);
    expect(truncateLeft("short.go", 12)).toBe("short.go");
    expect(truncateLeft("abc", 1)).toBe("");
  });
});

describe("footer and keys (DLG-301 to DLG-309)", () => {
  it("shortens the key hints as the sheet narrows (TestChangesFooterFitsCompactWindows)", () => {
    // A hint fits when it is at most the width minus 4 (changes.go:314-319).
    const [long, middle] = FOOTER_KEYS.map(cells);
    expect(footerKeys(long + 4)).toBe(FOOTER_KEYS[0]);
    expect(footerKeys(long + 3)).toBe(FOOTER_KEYS[1]);
    expect(footerKeys(middle + 4)).toBe(FOOTER_KEYS[1]);
    expect(footerKeys(middle + 3)).toBe(FOOTER_KEYS[2]);
    expect(footerKeys(20)).toBe(FOOTER_KEYS[2]);
    for (const width of [40, 68, 104, 140]) expect(footerKeys(width)).toContain("esc close");
  });
  it("maps the terminal's keys", () => {
    expect(["Escape", "Alt+Escape", "q"].map((k) => SHEET_KEYS[k])).toEqual(["close", "close", "close"]);
    expect(["ArrowUp", "k", "ArrowDown", "j"].map((k) => SHEET_KEYS[k])).toEqual(["up", "up", "down", "down"]);
    expect(["PageUp", "b", "Shift+Space", "PageDown", "Space", "f"].map((k) => SHEET_KEYS[k])).toEqual(["pageUp", "pageUp", "pageUp", "pageDown", "pageDown", "pageDown"]);
    expect(["Home", "g", "End", "Shift+G"].map((k) => SHEET_KEYS[k])).toEqual(["top", "top", "bottom", "bottom"]);
    expect(["n", "]", "Tab", "p", "[", "Shift+Tab"].map((k) => SHEET_KEYS[k])).toEqual(["next", "next", "next", "prev", "prev", "prev"]);
  });
  it("jumps between files without wrapping (DLG-304)", () => {
    const heads = [100, 400, 900];
    expect(jumpTarget(heads, 0, 1)).toBe(100);
    expect(jumpTarget(heads, 100, 1)).toBe(400);
    expect(jumpTarget(heads, 900, 1)).toBeNull();
    expect(jumpTarget(heads, 900, -1)).toBe(400);
    expect(jumpTarget(heads, 100, -1)).toBe(0);
  });
});

describe("loading (DLG-294, DLG-311; reviews.go)", () => {
  it("knows an expired or missing diff", () => {
    expect(isExpired(new Error("diff details are no longer saved (only the latest five prompts keep them)"))).toBe(true);
    expect(isExpired({ status: 404, message: "not found" })).toBe(true);
    expect(isExpired({ code: "not_found" })).toBe(true);
    expect(isExpired(new Error("connection refused"))).toBe(false);
    expect(EXPIRED_TEXT).toBe("This saved diff has expired. Full diffs are kept for the latest five prompts; the chat is still here.");
  });
  it("reads the calls of the sheet from the chat's messages, in the sheet's order", () => {
    const byId = new Map([
      ["m1", { parts: [{ type: "tool_call", data: { id: "c1", name: "edit", input: "{}" } }, { type: "text", data: { text: "hi" } }] }],
      ["m2", { parts: [{ type: "tool_result", data: { tool_call_id: "c1", content: "ok" } }, { type: "tool_call", data: { id: "c2", name: "bash", input: "{}" } }] }],
    ]);
    expect(callsFromChat({ ids: ["m1", "m2"], byId }, ["c2", "c1", "gone"])).toEqual([
      { call: { id: "c2", name: "bash", input: "{}" }, result: null },
      { call: { id: "c1", name: "edit", input: "{}" }, result: { tool_call_id: "c1", content: "ok" } },
    ]);
  });
  it("loads each saved summary once, from the review's own chat (TestReviewLoadsOffThread...)", async () => {
    const reads = [];
    const loadReview = async (sessionId, messageId) => {
      reads.push([sessionId, messageId]);
      return { id: messageId, parts: [{ type: "tool_result", data: { tool_call_id: "call", name: "bash", review: { changes: [{ path: "/workspace/file", after: { content: "new\n" } }] } } }] };
    };
    const summary = { tool_call_id: "call", name: "bash", review: { id: "result", session_id: "child", summary: { files: 1, paths: ["/workspace/file"] } } };
    const files = await loadSheetFiles("parent", [{ call: { id: "call", name: "bash", input: "{}" }, result: summary }], loadReview);
    expect(reads).toEqual([["child", "result"]]);
    expect(files.map((f) => f.path)).toEqual(["/workspace/file"]);
    const twice = await loadSheetFiles(
      "parent",
      [
        { call: { id: "call", name: "bash", input: "{}" }, result: { ...summary, review: { id: "r2", summary: { files: 1 } } } },
        { call: { id: "other", name: "bash", input: "{}" }, result: { tool_call_id: "other", review: { id: "r2", summary: { files: 1 } } } },
      ],
      loadReview,
    );
    expect(reads.slice(1)).toEqual([["parent", "r2"]]);
    expect(twice).toHaveLength(1);
  });
  it("passes a failed load on", async () => {
    const summary = { tool_call_id: "call", review: { id: "gone", summary: { files: 1 } } };
    await expect(loadSheetFiles("s", [{ call: { id: "call", name: "bash", input: "{}" }, result: summary }], async () => Promise.reject(new Error("diff details are no longer saved")))).rejects.toThrow("no longer saved");
  });
});

// CHAT-038 to CHAT-044: a `!` shell item (port of chat/shell.go and the
// cases of shell_test.go).
import { describe, expect, it } from "vitest";
import { exitMark, firstLines, lastLines, SHELL_MAX_LINES, SHELL_STEP, shellCommandLine, shellView, trimOutput } from "./shell.js";

const seq = (n) => Array.from({ length: n }, (_, i) => String(i + 1)).join("\n") + "\n";

describe("shell output windows (TestShellOutputWindows)", () => {
  it("keeps the first or last lines", () => {
    const output = "1\n2\n3\n4\n5";
    expect(firstLines(output, 2)).toBe("1\n2");
    expect(lastLines(output, 2)).toBe("4\n5");
    expect(firstLines(output, 10)).toBe(output);
    expect(lastLines(output, 10)).toBe(output);
    expect(firstLines(output, 0)).toBe("");
    expect(lastLines(output, 0)).toBe("");
    expect(lastLines("\nlast", 1)).toBe("last");
    expect(lastLines("\nlast", 2)).toBe("\nlast");
  });
});

describe("shell item", () => {
  it("shows the command on one line", () => {
    expect(shellCommandLine("echo a\necho\tb")).toBe("echo a echo    b");
    expect(shellView({ command: "ls", output: "", exitCode: null, running: true }).command).toBe("ls");
  });
  it("shows the activity mark alone until output streams (CHAT-039)", () => {
    expect(shellView({ command: "ping", output: "", running: true })).toMatchObject({ lines: [], spinner: true });
    const view = shellView({ command: "ping", output: "64 bytes from localhost: icmp_seq=0\n64 bytes from localhost: icmp_seq=1\n", running: true });
    expect(view.lines).toEqual(["64 bytes from localhost: icmp_seq=0", "64 bytes from localhost: icmp_seq=1"]);
    expect(view.spinner).toBe(true);
  });
  it("shows the newest 10 lines while streaming and counts the earlier ones (CHAT-041)", () => {
    const view = shellView({ command: "seq 30", output: seq(30), running: true });
    expect(view.lines).toHaveLength(SHELL_MAX_LINES);
    expect(view.lines.at(-1)).toBe("30");
    expect(view.above).toBe("… 20 earlier lines");
    expect(view.below).toBe("");
  });
  it("keeps a color code split across chunks (TestPendingShellItemPreservesANSIBoundaryInCollapsedTail)", () => {
    const view = shellView({ command: "seq 12", output: "1\n2\n\x1b[" + "31m3\x1b[0m\n4\n5\n6\n7\n8\n9\n10\n11\n12\n", running: true });
    expect(view.lines[0]).toBe("\x1b[31m3\x1b[0m");
    expect(view.above).toBe("… 2 earlier lines");
  });
  it("shows the first 10 lines when done and counts the rest (TestCompletedShellItemShowsHeadAndAccurateCount)", () => {
    const view = shellView({ command: "seq 12", output: "1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n11\n12\n\x1b[0m\n", exitCode: 0, running: false });
    expect(view.lines).toEqual(["1", "2", "3", "4", "5", "6", "7", "8", "9", "10"]);
    expect(view.below).toBe("… 2 more lines");
    expect(view.above).toBe("");
    expect(view.spinner).toBe(false);
  });
  it("shows 10 lines whole and 11 lines cut, and everything when expanded (CHAT-042)", () => {
    expect(shellView({ command: "x", output: seq(10), exitCode: 0 }).below).toBe("");
    expect(shellView({ command: "x", output: seq(11), exitCode: 0 }).below).toBe("… 1 more lines");
    const open = shellView({ command: "x", output: seq(25), exitCode: 0, expanded: true });
    expect(open.lines).toHaveLength(25);
    expect(open.below).toBe("");
  });
  it("applies carriage-return redraws after cutting", () => {
    expect(shellView({ command: "x", output: "10%\r50%\r100%\n", exitCode: 0 }).lines).toEqual(["100%"]);
  });
  it("marks a stop and a failure (CHAT-040)", () => {
    expect(exitMark(130, false)).toEqual({ text: "(Canceled)", tone: "cancelled" });
    expect(exitMark(2, false)).toEqual({ text: "(exit 2)", tone: "error" });
    expect(exitMark(0, false)).toBeNull();
    expect(exitMark(2, true)).toBeNull();
    expect(shellView({ command: "false", output: "", exitCode: 1 }).mark).toEqual({ text: "(exit 1)", tone: "error" });
  });
  it("trims trailing space and bare color resets", () => {
    expect(trimOutput("a\n\x1b[0m\n  \n")).toBe("a");
    expect(trimOutput("\x1b[31mred\x1b[0m")).toBe("\x1b[31mred");
    expect(shellView({ command: "x", output: " \n\x1b[0m\n", exitCode: 0 }).lines).toEqual([]);
  });
  it("moves the output five characters a step (CHAT-043)", () => {
    expect(SHELL_STEP).toBe(5);
  });
});

import { describe, expect, it } from "vitest";
import { parseLogLine } from "./loglines.js";

// Log lines as `crush logs` prints them (desktop-design-5-6 2.28, MISC-016).
describe("parseLogLine", () => {
  it("takes time, level and message, then the other keys by name", () => {
    const line = JSON.stringify({ time: "2026-10-19T15:04:05Z", level: "WARN", msg: "slow", source: { file: "a.go", line: 12.0 }, zeta: 1, alpha: "x" });
    const out = parseLogLine(line);
    expect(out.level).toBe("WARN");
    expect(out.msg).toBe("slow");
    expect(out.parts).toEqual([["alpha", "x"], ["source", "a.go:12"], ["zeta", "1"]]);
    expect(out.time).toMatch(/^\d\d:\d\d:\d\d$/);
  });

  it("skips lines that are not JSON objects, and treats an odd level as INFO", () => {
    expect(parseLogLine("panic: boom")).toBeNull();
    expect(parseLogLine("[1,2]")).toBeNull();
    expect(parseLogLine(JSON.stringify({ level: "TRACE", msg: "m" }))).toMatchObject({ level: "INFO", time: "" });
  });
});

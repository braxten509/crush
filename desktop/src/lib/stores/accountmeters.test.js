import { describe, expect, it } from "vitest";
import { accountGroups, listedKinds } from "./accountmeters.svelte.js";

const now = Date.parse("2026-10-21T12:00:00Z");
const accounts = [
  { id: "", kind: "claude-code", name: "Main", signed_in: true },
  { id: "c2", kind: "claude-code", name: "Work", signed_in: true },
  { id: "", kind: "codex-cli", name: "Codex 1", signed_in: true },
];
const reading = (kind, account, used5, usedWeek, extra = {}) => ({
  kind, account, state: "ready", read_at: "2026-10-21T11:59:00Z",
  windows: [{ name: "5h", used: used5 }, { name: "Weekly", used: usedWeek }, { name: "Weekly", used: 99, model: "opus" }],
  ...extra,
});

describe("account meters", () => {
  it("lists only tools with two or more sign-ins", () => {
    expect(listedKinds(accounts)).toEqual(["claude-code"]);
  });

  it("gives each account its part left, level, and in-use mark", () => {
    const byKind = { "claude-code": reading("claude-code", "", 0, 31), "claude-code#c2": reading("claude-code", "c2", 88, 95) };
    const [group] = accountGroups({ accounts, byKind, inUse: { kind: "claude-code", account: "" }, now });
    expect(group.title).toBe("Claude Code");
    expect(group.rows.map((r) => [r.name, r.inUse, r.state, r.five, r.week])).toEqual([
      ["Main", true, "ready", { left: 100, level: "" }, { left: 69, level: "" }],
      ["Work", false, "ready", { left: 12, level: "low" }, { left: 5, level: "out" }],
    ]);
  });

  it("marks a failed read with old numbers idle, and no reading checking", () => {
    const byKind = { "claude-code#c2": reading("claude-code", "c2", 50, 50, { state: "error", error: "claude login expired" }) };
    const rows = accountGroups({ accounts, byKind, now })[0].rows;
    expect(rows.map((r) => r.state)).toEqual(["checking", "idle"]);
    expect(rows[1].five).toEqual({ left: 50, level: "" });
  });
});

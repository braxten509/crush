import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const calls = vi.hoisted(() => ({ fallback: vi.fn(), refreshLimits: vi.fn() }));
vi.mock("../engine/api.js", () => ({ api: { fallback: calls.fallback, refreshLimits: calls.refreshLimits } }));
vi.mock("../engine/events.js", () => ({ onEvent: vi.fn(), onResync: vi.fn() }));
vi.mock("../engine/commands.js", () => ({ sign_in_run: vi.fn() }));

import { accountGroups, accountMeters, accountPollConnected, checkingKinds, listedKinds } from "./accountmeters.svelte.js";
import { fallback } from "../fallback/store.svelte.js";
import { limits } from "./limits.svelte.js";

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
  it("lists every sign-in tool, and a tool without sign-ins once it has a reading", () => {
    expect(listedKinds(accounts, {})).toEqual(["claude-code", "codex-cli"]);
    const never = { "agy-cli": { kind: "agy-cli", state: "error", error: "agy not found", windows: [] } };
    expect(listedKinds(accounts, never)).toEqual(["claude-code", "codex-cli"]);
    const read = { "agy-cli": reading("agy-cli", "", 0, 0), "kimi-coding": reading("kimi-coding", "", 0, 0) };
    expect(listedKinds(accounts, read)).toEqual(["claude-code", "codex-cli", "agy-cli", "kimi-coding"]);
  });

  it("gives a tool with one sign-in its own group", () => {
    const byKind = { "codex-cli": reading("codex-cli", "", 29, 60) };
    const codex = accountGroups({ accounts, byKind, now }).find((g) => g.kind === "codex-cli");
    expect(codex.title).toBe("Codex");
    expect(codex.rows.map((r) => [r.name, r.state, r.parts])).toEqual([["Codex 1", "ready", [{ left: 71, level: "" }, { left: 40, level: "" }]]]);
  });

  it("heads each tool with its own spans, 5h, Week, Month first", () => {
    const grokAccounts = [...accounts, { id: "", kind: "grok-cli", name: "Grok", signed_in: true }];
    const byKind = {
      "grok-cli": { kind: "grok-cli", state: "ready", read_at: "2026-10-21T11:59:00Z", windows: [{ name: "Monthly", used: 42 }] },
      "opencode-cli": { kind: "opencode-cli", state: "ready", read_at: "2026-10-21T11:59:00Z", windows: [{ name: "Monthly", used: 40 }, { name: "5h", used: 5 }, { name: "Weekly", used: 23 }] },
    };
    const groups = accountGroups({ accounts: grokAccounts, byKind, now });
    expect(groups.map((g) => [g.title, g.spans])).toEqual([
      ["Claude Code", []],
      ["Codex", []],
      ["Grok", ["Month"]],
      ["OpenCode", ["5h", "Week", "Month"]],
    ]);
    expect(groups[3].rows.map((r) => [r.name, r.parts.map((p) => p.left)])).toEqual([["OpenCode", [95, 77, 60]]]);
  });

  it("shows the lowest model for a tool that counts each model apart", () => {
    const byKind = {
      "agy-cli": { kind: "agy-cli", state: "ready", read_at: "2026-10-21T11:59:00Z", windows: [
        { name: "5h", used: 10, model: "flash" }, { name: "5h", used: 75, model: "pro" }, { name: "Weekly", used: 20, model: "flash" },
      ] },
    };
    const [agy] = accountGroups({ accounts: [], byKind, inUse: { kind: "agy-cli", account: "" }, now });
    expect([agy.title, agy.spans, agy.rows[0].inUse, agy.rows[0].parts]).toEqual(["Antigravity", ["5h", "Week"], true, [{ left: 25, level: "low" }, { left: 80, level: "" }]]);
  });

  it("gives each account its part left, level, and in-use mark", () => {
    const byKind = { "claude-code": reading("claude-code", "", 0, 31), "claude-code#c2": reading("claude-code", "c2", 88, 95) };
    const [group] = accountGroups({ accounts, byKind, inUse: { kind: "claude-code", account: "" }, now });
    expect([group.title, group.spans]).toEqual(["Claude Code", ["5h", "Week"]]);
    expect(group.rows.map((r) => [r.name, r.inUse, r.state, r.parts])).toEqual([
      ["Main", true, "ready", [{ left: 100, level: "" }, { left: 69, level: "" }]],
      ["Work", false, "ready", [{ left: 12, level: "low" }, { left: 5, level: "out" }]],
    ]);
  });

  it("marks a failed read with old numbers idle, and no reading checking", () => {
    const byKind = { "claude-code#c2": reading("claude-code", "c2", 50, 50, { state: "error", error: "claude login expired" }) };
    const rows = accountGroups({ accounts, byKind, now })[0].rows;
    expect(rows.map((r) => r.state)).toEqual(["checking", "idle"]);
    expect(rows[1].parts).toEqual([{ left: 50, level: "" }, { left: 50, level: "" }]);
  });

  it("stops saying checking when the first read failed", () => {
    const byKind = { "claude-code#c2": reading("claude-code", "c2", 0, 0, { read_at: undefined, state: "error", error: "claude login expired" }) };
    const rows = accountGroups({ accounts, byKind, now })[0].rows;
    expect(rows.map((r) => r.state)).toEqual(["checking", "error"]);
  });

  it("names the tools with a signed-in account still checking", () => {
    const byKind = { "claude-code": reading("claude-code", "", 0, 31), "codex-cli": reading("codex-cli", "", 0, 0) };
    expect(checkingKinds(accounts, byKind)).toEqual(["claude-code"]);
    byKind["claude-code#c2"] = reading("claude-code", "c2", 50, 50);
    expect(checkingKinds(accounts, byKind)).toEqual([]);
    const signedOut = accounts.map((a) => (a.id === "c2" ? { ...a, signed_in: false } : a));
    expect(checkingKinds(signedOut, { "claude-code": reading("claude-code", "", 0, 31), "codex-cli": reading("codex-cli", "", 0, 0) })).toEqual([]);
  });
});

describe("the account meter timer", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(now);
    calls.refreshLimits.mockReset();
    calls.refreshLimits.mockResolvedValue(undefined);
    fallback.loaded = true;
    fallback.accounts = accounts;
    limits.byKind = { "claude-code": reading("claude-code", "", 0, 31) };
    accountMeters.on = true;
  });

  afterEach(() => {
    accountPollConnected(false);
    accountMeters.on = false;
    vi.useRealTimers();
  });

  it("asks again for a Claude account still checking after three minutes", async () => {
    accountPollConnected(true);
    await Promise.resolve(); // the first read after the accounts load
    calls.refreshLimits.mockClear();

    await vi.advanceTimersByTimeAsync(60_000);
    expect(calls.refreshLimits).not.toHaveBeenCalledWith("claude-code");
    await vi.advanceTimersByTimeAsync(2 * 60_000);
    expect(calls.refreshLimits).toHaveBeenCalledWith("claude-code");
  });
});

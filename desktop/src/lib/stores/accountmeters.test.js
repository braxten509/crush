import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const calls = vi.hoisted(() => ({ fallback: vi.fn(), refreshLimits: vi.fn(), authState: vi.fn() }));
vi.mock("../engine/api.js", () => ({ api: { fallback: calls.fallback, refreshLimits: calls.refreshLimits, authState: calls.authState } }));
vi.mock("../engine/events.js", () => ({ onEvent: vi.fn(), onResync: vi.fn() }));
vi.mock("../engine/commands.js", () => ({ sign_in_run: vi.fn() }));

import { signedOutCLIs } from "../accounts/clis.js";
import { cliState } from "../accounts/state.svelte.js";
import { buildGroups } from "../models/groups.js";
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

  it("gives a tool with one sign-in one row with its parts left", () => {
    const byKind = { "codex-cli": reading("codex-cli", "", 29, 60) };
    const codex = accountGroups({ accounts, byKind, now }).find((g) => g.kind === "codex-cli");
    expect(codex.rows.map((r) => [r.id, r.email, r.state, r.parts])).toEqual([["", "", "ready", [{ left: 71, level: "" }, { left: 40, level: "" }, null]]]);
  });

  it("shows a login's email on hover, from its email or a name that is one", () => {
    const logins = [
      { id: "", kind: "claude-code", name: "Personal", email: "me@example.com", signed_in: true },
      { id: "c2", kind: "claude-code", name: "work@example.com", signed_in: true },
      { id: "", kind: "grok-cli", name: "Grok 1", signed_in: true },
    ];
    const rows = accountGroups({ accounts: logins, now }).flatMap((g) => g.rows);
    expect(rows.map((r) => [r.name, r.email])).toEqual([["Claude", "me@example.com"], ["Claude 2", "work@example.com"], ["Grok", ""]]);
  });

  it("puts every tool in the same 5h, Week, Month columns", () => {
    const grokAccounts = [...accounts, { id: "", kind: "grok-cli", name: "Grok 1", signed_in: true }];
    const byKind = {
      "grok-cli": { kind: "grok-cli", state: "ready", read_at: "2026-10-21T11:59:00Z", windows: [{ name: "Monthly", used: 42 }] },
      "opencode-cli": { kind: "opencode-cli", state: "ready", read_at: "2026-10-21T11:59:00Z", windows: [{ name: "Monthly", used: 40 }, { name: "5h", used: 5 }, { name: "Weekly", used: 23 }, { name: "3d", used: 1 }] },
    };
    const groups = accountGroups({ accounts: grokAccounts, byKind, now });
    expect(groups.slice(2).map((g) => [g.kind, g.rows.map((r) => r.parts.map((p) => p?.left ?? null))])).toEqual([
      ["grok-cli", [[null, null, 58]]],
      ["opencode-cli", [[95, 77, 60]]],
    ]);
  });

  it("shows Antigravity's lowest Gemini model, an untouched one in 5h", () => {
    const byKind = {
      "agy-cli": { kind: "agy-cli", state: "ready", read_at: "2026-10-21T11:59:00Z", windows: [
        { name: "5h", used: 10, model: "gemini-3-flash" }, { name: "Limit", used: 0, model: "gemini-3-pro-high" },
        { name: "5h", used: 75, model: "claude-sonnet-4-6" }, { name: "Weekly", used: 87, model: "claude-opus-4-6-thinking" },
      ] },
    };
    const [agy] = accountGroups({ accounts: [], byKind, inUse: { kind: "agy-cli", account: "" }, now });
    expect([agy.rows[0].name, agy.rows[0].inUse, agy.rows[0].parts]).toEqual(["Agy", true, [{ left: 90, level: "" }, null, null]]);
  });

  it("gives a model family with a limit of its own a line under its account (owner, 2026-11-12)", () => {
    const byKind = {
      "claude-code": { kind: "claude-code", state: "ready", read_at: "2026-10-21T11:59:00Z", windows: [
        { name: "5h", used: 26 }, { name: "Weekly", used: 58 }, { name: "Weekly Fable", used: 43, model: "Fable" },
      ] },
      "agy-cli": { kind: "agy-cli", state: "ready", read_at: "2026-10-21T11:59:00Z", windows: [
        { name: "Weekly", used: 10, model: "gemini" }, { name: "5h", used: 0, model: "gemini" },
        { name: "Weekly", used: 30, model: "Claude Opus" }, { name: "5h", used: 99, model: "Claude Opus" },
        { name: "Weekly", used: 40, model: "Gemini 4 Argon" },
      ] },
    };
    const lefts = (parts) => parts.map((p) => p?.left ?? null);
    const lines = accountGroups({ accounts: accounts.slice(0, 1), byKind, now }).map((g) => {
      const row = g.rows[0];
      return [row.name, lefts(row.parts), row.families.map((f) => [f.name, lefts(f.parts)])];
    });
    expect(lines).toEqual([
      ["Claude", [74, 42, null], [["Fable", [null, 57, null]]]],
      ["Agy", [100, 90, null], [["Argon", [null, 60, null]], ["Claude", [1, 70, null]]]],
    ]);
  });

  it("lists a tool's sign-ins in order, with each one's part left, level, and in-use mark", () => {
    const byKind = { "claude-code": reading("claude-code", "", 0, 31), "claude-code#c2": reading("claude-code", "c2", 88, 95) };
    const [group] = accountGroups({ accounts, byKind, inUse: { kind: "claude-code", account: "" }, now });
    expect(group.rows.map((r) => [r.id, r.email, r.inUse, r.state, r.parts])).toEqual([
      ["", "", true, "ready", [{ left: 100, level: "" }, { left: 69, level: "" }, null]],
      ["c2", "", false, "ready", [{ left: 12, level: "low" }, { left: 5, level: "out" }, null]],
    ]);
  });

  it("marks a failed read with old numbers idle, and no reading checking", () => {
    const byKind = { "claude-code#c2": reading("claude-code", "c2", 50, 50, { state: "error", error: "claude login expired" }) };
    const rows = accountGroups({ accounts, byKind, now })[0].rows;
    expect(rows.map((r) => r.state)).toEqual(["checking", "idle"]);
    expect(rows[1].parts).toEqual([{ left: 50, level: "" }, { left: 50, level: "" }, null]);
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

  it("hides signed-out accounts and unsigned OpenCode Go from the usage list and model picker", () => {
    const logins = [
      { id: "", kind: "claude-code", name: "Claude 1", signed_in: false },
      { id: "c2", kind: "claude-code", name: "Claude 2", signed_in: true },
      { id: "", kind: "grok-cli", name: "Grok 1", signed_in: false },
    ];
    const byKind = {
      "claude-code#c2": reading("claude-code", "c2", 10, 20),
      "grok-cli": reading("grok-cli", "", 15, 25),
      "opencode-cli": reading("opencode-cli", "", 0, 4, { error: "OpenCode Console is not signed in" }),
      "kimi-coding": reading("kimi-coding", "", 0, 25),
    };
    const providers = new Map([
      ["claude-code", { id: "claude-code", name: "Claude Code", type: "claude-code", models: [{ id: "opus", name: "Opus" }] }],
      ["grok-cli", { id: "grok-cli", name: "Grok", type: "grok-cli", models: [{ id: "grok-4.7", name: "Grok 4.7" }] }],
      ["opencode-cli", { id: "opencode-cli", name: "OpenCode Go", type: "opencode-cli", models: [{ id: "opencode-go/glm-5.3", name: "GLM 5.3" }] }],
      ["opencode-zen-cli", { id: "opencode-zen-cli", name: "OpenCode Zen", type: "opencode-zen-cli", models: [{ id: "opencode/big-pickle", name: "Big Pickle" }] }],
      ["kimi-coding", { id: "kimi-coding", name: "Kimi", type: "kimi-coding", has_api_key: false, models: [] }],
    ]);
    const cliSignedIn = { "grok-cli": false, "opencode-cli": false, "opencode-zen-cli": false };

    const groups = accountGroups({ accounts: logins, byKind, now, cliSignedIn, providers });
    expect(groups.map((g) => [g.kind, g.rows.map((r) => r.id)])).toEqual([["claude-code", ["c2"]]]);

    const hidden = signedOutCLIs({ accounts: logins, accountsLoaded: true, cliSignedIn, cliLoaded: true, cfg: { providers } });
    expect([...hidden].sort()).toEqual(["grok-cli", "opencode-cli"]);
    const picker = buildGroups({ catalog: [], cfg: { providers, recent_models: { large: [{ provider: "grok-cli", model: "grok-4.7" }] } }, type: "large", hidden });
    expect(picker.items.map((item) => item.provider.id)).toEqual(["claude-code", "opencode-zen-cli"]);
    expect(picker.prunedRecent).toBeNull();
  });
});

describe("the account meter timer", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(now);
    calls.refreshLimits.mockReset();
    calls.refreshLimits.mockResolvedValue(undefined);
    calls.authState.mockReset();
    calls.authState.mockResolvedValue({ providers: {}, emails: {} });
    fallback.loaded = true;
    fallback.accounts = accounts;
    cliState.loaded = true;
    cliState.providers = {};
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
    expect(calls.refreshLimits).not.toHaveBeenCalledWith("claude-code", expect.anything());
    await vi.advanceTimersByTimeAsync(2 * 60_000);
    expect(calls.refreshLimits).toHaveBeenCalledWith("claude-code", expect.anything());
  });
});

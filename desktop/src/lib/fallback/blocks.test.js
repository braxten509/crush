import { describe, expect, it } from "vitest";
import { blockText, changeBlock, fallbackProviders, leaveText, minutesText, moveBlock, newBlock } from "./blocks.js";
import { meterModel } from "../footer.js";

const providers = [
  { id: "claude-code", type: "claude-code", name: "Claude Code", models: [{ id: "opus", name: "Claude Opus" }] },
  { id: "codex-cli", type: "codex-cli", name: "Codex", models: [{ id: "gpt-5.6-sol", name: "GPT-5.6 Sol" }] },
];
const accounts = [
  { id: "", kind: "claude-code", name: "Claude 1" },
  { id: "claude-2", kind: "claude-code", name: "Claude 2" },
  { id: "", kind: "codex-cli", name: "Codex 1" },
];
const ctx = { accounts, providers };

describe("the fallback order's blocks", () => {
  it("say what they do in one line", () => {
    expect(blockText({ type: "use", provider: "claude-code", model: "opus", account: "claude-2" }, ctx)).toBe("Use Claude 2 · Claude Opus");
    expect(blockText({ type: "use", provider: "codex-cli", model: "gpt-5.6-sol", effort: "high" }, ctx)).toBe("Use Codex 1 · GPT-5.6 Sol · high");
    expect(blockText({ type: "wait", kind: "claude-code", max_wait_minutes: 90, meanwhile: "next" }, ctx)).toBe(
      "Wait for Claude 1 to refill, at most 1 h 30 min, using the next blocks meanwhile",
    );
    expect(blockText({ type: "refill", kind: "codex-cli" }, ctx)).toBe("Use a free refill on Codex 1");
    expect(minutesText(60)).toBe("1 h");
    expect(minutesText(45)).toBe("45 min");
  });

  it("say how the chat leaves a use block", () => {
    expect(leaveText({ type: "use" })).toBe("when it runs out");
    expect(leaveText({ type: "use", move_at_percent: 95, refill_first: true, come_back: true })).toBe("at 95% used · after a free refill · comes back when it refills");
    expect(leaveText({ type: "wait" })).toBe("");
  });

  it("move, and keep only the fields their type has", () => {
    const blocks = ["a", "b", "c"];
    expect(moveBlock(blocks, 0, 2)).toEqual(["b", "c", "a"]);
    expect(moveBlock(blocks, 2, 0)).toEqual(["c", "a", "b"]);
    expect(moveBlock(blocks, 1, 9)).toEqual(["a", "c", "b"]);
    // Empty values are left out, as the engine writes them.
    expect(changeBlock({ type: "use", provider: "claude-code", model: "opus", move_at_percent: 95 }, { move_at_percent: 0, account: "" })).toEqual({
      type: "use",
      provider: "claude-code",
      model: "opus",
    });
    // A wait block drops what only a use block has.
    expect(changeBlock({ type: "wait", kind: "claude-code", come_back: true }, { max_wait_minutes: 60 })).toEqual({ type: "wait", kind: "claude-code", max_wait_minutes: 60 });
  });

  it("start from the first agent CLI", () => {
    expect(newBlock("use", { providers })).toEqual({ type: "use", provider: "claude-code", model: "opus", account: "" });
    expect(newBlock("refill").kind).toBe("codex-cli");
  });
});

describe("the providers a use block may pick", () => {
  it("are the configured ones, agent CLIs included, with catalog models added", () => {
    const cfg = {
      providers: new Map([
        ["claude-code", { name: "Claude Code", type: "claude-code", models: [{ id: "opus", name: "Opus" }] }],
        ["openai", { name: "", type: "", models: [] }],
        ["off", { name: "Off", type: "openai", disable: true, models: [{ id: "x" }] }],
        ["empty", { name: "Empty", type: "openai", models: [] }],
      ]),
    };
    const catalog = [
      { id: "openai", name: "OpenAI", type: "openai", models: [{ id: "gpt" }] },
      { id: "anthropic", name: "Anthropic", type: "anthropic", models: [{ id: "sonnet" }] },
    ];
    expect(fallbackProviders(cfg, catalog)).toEqual([
      { id: "claude-code", name: "Claude Code", type: "claude-code", models: [{ id: "opus", name: "Opus" }] },
      { id: "openai", name: "OpenAI", type: "openai", models: [{ id: "gpt" }] },
    ]);
    expect(fallbackProviders(null, catalog)).toEqual([]);
  });
});

describe("the refill meter", () => {
  it("shows only while the account has free refills", () => {
    const none = meterModel({ items: [], limits: [], refills: 0 });
    expect(none.meters.find((m) => m.id === "refills")).toBeUndefined();
    const two = meterModel({ items: [], limits: [], refills: 2 });
    expect(two.meters.find((m) => m.id === "refills")).toMatchObject({ label: "REFILLS", text: "2", bar: false, refill: true });
    expect(two.lines).toContain("2 free refills");
  });
});

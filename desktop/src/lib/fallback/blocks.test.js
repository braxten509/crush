import { describe, expect, it } from "vitest";
import { changeBlock, fallbackProviders, kindsToRefresh, menuProviders, moveBlock, newBlock, sentence } from "./blocks.js";
import { meterModel } from "../footer.js";

const providers = [
  { id: "claude-code", type: "claude-code", name: "Claude Code", models: [{ id: "opus", name: "Claude Opus", reasoning_levels: ["low", "high"] }] },
  { id: "codex-cli", type: "codex-cli", name: "Codex", models: [{ id: "gpt-5.6-sol", name: "GPT-5.6 Sol" }] },
];
const accounts = [
  { id: "", kind: "claude-code", name: "Claude 1" },
  { id: "claude-2", kind: "claude-code", name: "Claude 2" },
  { id: "", kind: "codex-cli", name: "Codex 1" },
];
const ctx = { accounts, providers };

describe("the fallback order's blocks", () => {
  it("read as sentences whose words open menus", () => {
    const menus = (block) => sentence(block, ctx).filter((piece) => piece.menu).map((piece) => piece.menu);
    // Claude Code has two accounts, so its account word opens a menu; the
    // model's effort levels give it an effort word.
    const claude = { type: "use", provider: "claude-code", model: "opus", account: "claude-2" };
    expect(menus(claude)).toEqual(["model", "account", "effort", "leave"]);
    // Codex has one account: plain words. The extras join the leave word.
    const codex = { type: "use", provider: "codex-cli", model: "gpt-5.6-sol", move_at_percent: 95, refill_first: true, come_back: true };
    expect(menus(codex)).toEqual(["model", "leave"]);
    const wait = { type: "wait", kind: "claude-code", max_wait_minutes: 90, meanwhile: "next" };
    expect(menus(wait)).toEqual(["account", "wait", "meanwhile"]);
    // A refill block picks among the Codex accounts only: one, so no menu.
    expect(menus({ type: "refill", kind: "codex-cli" })).toEqual([]);
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

describe("account usage refreshes", () => {
  it("ask for each tool whose account was added or just signed in", () => {
    const first = [
      { id: "", kind: "claude-code", signed_in: true },
      { id: "claude-2", kind: "claude-code", signed_in: false },
    ];
    const signedIn = [
      { id: "", kind: "claude-code", signed_in: true },
      { id: "claude-2", kind: "claude-code", signed_in: true },
      { id: "", kind: "codex-cli", signed_in: true },
    ];
    expect(kindsToRefresh([], first)).toEqual(["claude-code"]);
    expect(kindsToRefresh(first, signedIn)).toEqual(["claude-code", "codex-cli"]);
    expect(kindsToRefresh(signedIn, signedIn)).toEqual([]);
    expect(kindsToRefresh(first, [{ id: "claude-3", kind: "claude-code", signed_in: false }])).toEqual([]);
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

describe("the providers the model menu offers", () => {
  it("are every configured provider, the agent CLIs first", () => {
    const providers = [
      { id: "openrouter", name: "OpenRouter", type: "openrouter", models: [{ id: "m1" }] },
      { id: "claude-code", name: "Claude Code", type: "claude-code", models: [{ id: "opus" }] },
      { id: "zag", name: "Antigravity", type: "agy-cli", models: [{ id: "gemini" }] },
      { id: "anthropic", name: "Anthropic", type: "anthropic", models: [{ id: "sonnet" }] },
    ];
    expect(menuProviders(providers).map((p) => p.id)).toEqual(["claude-code", "zag", "openrouter", "anthropic"]);
    expect(menuProviders([])).toEqual([]);
    expect(menuProviders(null)).toEqual([]);
  });
});

describe("the refill meter", () => {
  it("shows only while the account has free refills", () => {
    const none = meterModel({ items: [], limits: [], refills: 0 });
    expect(none.meters.find((m) => m.id === "refills")).toBeUndefined();
    const two = meterModel({ items: [], limits: [], refills: 2 });
    expect(two.meters.find((m) => m.id === "refills")).toBeDefined();
  });
});

describe("per-model lists", () => {
  it("each model's list is edited on its own; every other model keeps order.blocks", async () => {
    const { addList, listBlocks, listKey, listsOf, removeList, withListBlocks } = await import("./blocks.js");
    const opus = { type: "use", provider: "claude-code", model: "opus" };
    let order = { on: true, blocks: [opus] };
    order = addList(order, "codex-cli", "gpt-5.6-sol");
    const gpt = listKey("codex-cli", "gpt-5.6-sol");
    expect(listsOf(order).map((l) => l.key)).toEqual([gpt, ""]);
    expect(listBlocks(order, gpt)).toEqual([{ type: "use", provider: "codex-cli", model: "gpt-5.6-sol" }]);
    order = withListBlocks(order, gpt, [...listBlocks(order, gpt), { ...opus, account: "claude-2" }]);
    expect(listBlocks(order, gpt).map((b) => b.account ?? "")).toEqual(["", "claude-2"]);
    expect(listBlocks(order, "")).toEqual([opus]);
    expect(addList(order, "codex-cli", "gpt-5.6-sol")).toBe(order); // one list per model
    order = removeList(order, gpt);
    expect([order.lists, listBlocks(order, gpt)]).toEqual([[], [opus]]);
  });
});

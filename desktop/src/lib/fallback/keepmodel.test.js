import { describe, expect, it } from "vitest";
import { accountChoices, keepChatModel, pickModel, sentence } from "./blocks.js";

const providers = [
  { id: "claude-code", type: "claude-code", name: "Claude Code", models: [{ id: "opus", name: "Claude Opus", reasoning_levels: ["low", "high"] }] },
  { id: "codex-cli", type: "codex-cli", name: "Codex", models: [{ id: "gpt-5.6-sol", name: "GPT-5.6 Sol" }] },
];
const accounts = [
  { id: "", kind: "claude-code", name: "Claude 1" },
  { id: "claude-2", kind: "claude-code", name: "Claude 2" },
  { id: "", kind: "codex-cli", name: "Codex 1" },
  { id: "codex-2", kind: "codex-cli", name: "Codex 2" },
];
const ctx = { accounts, providers };

describe("a block that keeps the chat's model", () => {
  it("is saved with only a tool and an account, and turns back into a model's block", () => {
    const named = { type: "use", provider: "claude-code", model: "opus", effort: "high", account: "claude-2", come_back: true };
    const kept = keepChatModel(named, providers);
    expect(kept).toEqual({ type: "use", keep_model: true, kind: "claude-code", account: "claude-2", come_back: true });
    // It has no effort word, and its account word offers every tool's accounts.
    expect(sentence(kept, ctx).map((piece) => piece.menu).filter(Boolean)).toEqual(["model", "account", "leave"]);
    expect(accountChoices(kept, ctx)).toHaveLength(4);
    // Picking a model of the same tool keeps the account; another tool's does not.
    expect(pickModel(kept, providers, "claude-code", "opus")).toEqual({ type: "use", provider: "claude-code", model: "opus", account: "claude-2", come_back: true });
    expect(pickModel(kept, providers, "codex-cli", "gpt-5.6-sol")).toEqual({ type: "use", provider: "codex-cli", model: "gpt-5.6-sol", come_back: true });
  });
});

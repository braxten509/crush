import { describe, expect, it } from "vitest";
import { autoCompactTokenLimit, compactionLimit, fallbackCompactionLimit, nativeCompaction, providerKind, zenFreeDeepSeekFullContext } from "./compaction.js";

// SIDE-015: the port of composer_footer.go:183-207 and config/autocompact.go.
const agent = (provider, model = "opus", window = 200_000) => ({
  is_ready: true,
  model: { id: model, name: "Opus 5.5", context_window: window },
  model_cfg: { model, provider },
});
const budget = (provider, model, tokens, estimated = false) => ({ context_budget: { provider, model, tokens, estimated } });

describe("compactionLimit", () => {
  it("an agent CLI uses the chat's budget when it is for the same CLI and model", () => {
    for (const kind of ["claude-code", "codex-cli", "opencode-cli", "grok-cli", "agy-cli"]) {
      expect(compactionLimit({ agent: agent(kind), session: budget(kind, "opus", 200_000) })).toBe(200_000);
    }
  });

  it("an agent CLI without a matching budget is unknown (0), never the fallback", () => {
    expect(compactionLimit({ agent: agent("claude-code"), session: null })).toBe(0);
    expect(compactionLimit({ agent: agent("claude-code"), session: {} })).toBe(0);
    expect(compactionLimit({ agent: agent("claude-code"), session: budget("claude-code", "sonnet", 200_000) })).toBe(0);
    expect(compactionLimit({ agent: agent("claude-code"), session: budget("codex-cli", "opus", 200_000) })).toBe(0);
  });

  it("the provider's type comes from the providers list; the budget names the type", () => {
    const providers = [{ id: "work-claude", name: "Work Claude", type: "claude-code" }];
    const a = agent("work-claude");
    expect(compactionLimit({ agent: a, providers, session: budget("claude-code", "opus", 150_000) })).toBe(150_000);
    expect(compactionLimit({ agent: a, providers, session: budget("work-claude", "opus", 150_000) })).toBe(0);
    // Without the list the id stands for the type: an API provider's fallback.
    expect(compactionLimit({ agent: a, session: budget("claude-code", "opus", 150_000) })).toBe(160_000);
  });

  it("an API model uses the smaller of the configured limit and the window minus its buffer", () => {
    expect(compactionLimit({ agent: agent("openai-compat", "fake", 200_000) })).toBe(160_000);
    expect(compactionLimit({ agent: agent("openai-compat", "fake", 200_001) })).toBe(180_001);
    expect(compactionLimit({ agent: agent("openai-compat", "fake", 1_000_000) })).toBe(400_000);
    expect(compactionLimit({ agent: agent("openai-compat", "fake", 300_000), options: { auto_compact_token_limit: 100_000 } })).toBe(100_000);
    expect(compactionLimit({ agent: agent("openai-compat", "fake", 0), options: { auto_compact_token_limit: 90_000 } })).toBe(90_000);
    // A budget means nothing to an API model.
    expect(compactionLimit({ agent: agent("openai-compat", "fake", 200_000), session: budget("openai-compat", "fake", 5) })).toBe(160_000);
  });

  it("no agent model: the configured limit", () => {
    expect(compactionLimit({})).toBe(400_000);
    expect(compactionLimit({ agent: { is_ready: false }, options: { auto_compact_token_limit: 50_000 } })).toBe(50_000);
  });
});

describe("the config rules", () => {
  it("fallbackCompactionLimit", () => {
    expect(fallbackCompactionLimit(200_000, 400_000)).toBe(160_000);
    expect(fallbackCompactionLimit(7, 400_000)).toBe(6); // integer division, as Go's int64
    expect(fallbackCompactionLimit(250_000, 400_000)).toBe(230_000);
    expect(fallbackCompactionLimit(-1, 123)).toBe(123);
    expect(fallbackCompactionLimit(0, 123)).toBe(123);
  });

  it("autoCompactTokenLimit keeps only a positive saved threshold", () => {
    expect(autoCompactTokenLimit(undefined)).toBe(400_000);
    expect(autoCompactTokenLimit({ auto_compact_token_limit: 0 })).toBe(400_000);
    expect(autoCompactTokenLimit({ auto_compact_token_limit: -5 })).toBe(400_000);
    expect(autoCompactTokenLimit({ auto_compact_token_limit: 12_345 })).toBe(12_345);
  });

  it("nativeCompaction and providerKind", () => {
    expect(nativeCompaction("claude-code")).toBe(true);
    expect(nativeCompaction("openai-compat")).toBe(false);
    expect(nativeCompaction("")).toBe(false);
    expect(providerKind("x", [{ id: "x", type: "codex-cli" }])).toBe("codex-cli");
    expect(providerKind("y", [{ id: "x", type: "codex-cli" }])).toBe("y");
    expect(providerKind("", null)).toBe("");
  });
});

// The switch that keeps OpenCode Zen's free DeepSeek on its whole window
// (options.opencode_zen_free_deepseek_full_context, owner 2026-10-23).
describe("zenFreeDeepSeekFullContext", () => {
  it("is on only when it was turned on", () => {
    expect(zenFreeDeepSeekFullContext(undefined)).toBe(false);
    expect(zenFreeDeepSeekFullContext({})).toBe(false);
    expect(zenFreeDeepSeekFullContext({ opencode_zen_free_deepseek_full_context: false })).toBe(false);
    expect(zenFreeDeepSeekFullContext({ opencode_zen_free_deepseek_full_context: true })).toBe(true);
  });
});

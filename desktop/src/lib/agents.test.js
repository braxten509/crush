import { describe, expect, it } from "vitest";
import { AGENT_NAMES, agentName, chatAgent } from "./agents.js";

describe("agentName", () => {
  it("names each agent CLI", () => {
    expect(agentName("claude-code")).toBe("Claude");
    expect(agentName("codex-cli")).toBe("Codex");
    expect(agentName("grok-cli")).toBe("Grok");
    expect(agentName("agy-cli")).toBe("Antigravity");
    expect(agentName("opencode-cli")).toBe("OpenCode");
    expect(Object.keys(AGENT_NAMES)).toHaveLength(5);
  });

  it("names an API provider from the providers list, else by its id", () => {
    const providers = [{ id: "anthropic", name: "Anthropic" }];
    expect(agentName("anthropic", providers)).toBe("Anthropic");
    expect(agentName("openrouter", providers)).toBe("openrouter");
  });

  it("gives no name without a provider, and never one from the object prototype", () => {
    expect(agentName("")).toBe("");
    expect(agentName(undefined)).toBe("");
    expect(agentName("constructor")).toBe("constructor");
  });
});

describe("chatAgent", () => {
  it("prefers the context budget's provider", () => {
    const session = { id: "s1", context_budget: { provider: "codex-cli", model: "gpt-6-sol", tokens: 1, estimated: true } };
    expect(chatAgent(session, { provider: "claude-code", model: "opus" })).toBe("Codex");
  });

  it("falls back to the last loaded model, else shows no agent line", () => {
    expect(chatAgent({ id: "s1" }, { provider: "grok-cli", model: "grok-4.7" })).toBe("Grok");
    expect(chatAgent({ id: "s1" })).toBe("");
    expect(chatAgent({ id: "s1", context_budget: { provider: "" } }, null)).toBe("");
  });
});

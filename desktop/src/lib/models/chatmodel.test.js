import { describe, expect, it } from "vitest";
import { agentWithModel, shownChatModel } from "./chatmodel.js";

// The model a chat runs on, so a model picked in another chat never changes
// this one (owner, 2026-10-23).
const providers = [
  { id: "claude-code", models: [{ id: "opus", name: "Opus 5.5", context_window: 400_000 }] },
  { id: "codex-cli", models: [{ id: "gpt-6-sol", name: "GPT-6 Sol", context_window: 400_000 }] },
];
// The agent info carries no model (owner, 2026-10-24: no shared model).
const agent = { is_ready: true, is_busy: false, agent: "coder" };

describe("shownChatModel", () => {
  it("is the chat's saved model, or null", () => {
    expect(shownChatModel({ id: "s" })).toBe(null);
    expect(shownChatModel({ id: "s", model: {} })).toBe(null);
    expect(shownChatModel({ id: "s", model: { provider: "p" } })).toBe(null);
    expect(shownChatModel({ id: "s", model: { provider: "codex-cli", model: "gpt-6-sol" } })).toEqual({ provider: "codex-cli", model: "gpt-6-sol" });
  });
});

describe("agentWithModel", () => {
  it("shows the chat's own model and its settings", () => {
    const chat = { model: { provider: "codex-cli", model: "gpt-6-sol", reasoning_effort: "max", service_tier: "fast" } };
    const shown = agentWithModel(agent, chat, providers);
    expect(shown.model_cfg).toEqual(chat.model);
    expect(shown.model.name).toBe("GPT-6 Sol");
    expect(shown.is_ready).toBe(true);
  });

  it("shows no model when the chat has none and no last pick is given", () => {
    expect(agentWithModel(agent, { id: "s" }, providers)).toEqual({ ...agent, model: null, model_cfg: null });
    expect(agentWithModel(null, null, providers)).toBe(null);
    const before = agentWithModel(null, { model: { provider: "codex-cli", model: "gpt-6-sol" } }, providers);
    expect(before.model.name).toBe("GPT-6 Sol");
  });

  it("names a model this window has not read by its id, never the agent's", () => {
    const shown = agentWithModel(agent, { model: { provider: "gone", model: "gone-model" } }, providers);
    expect(shown.model_cfg).toEqual({ provider: "gone", model: "gone-model" });
    expect(shown.model.name).toBe("gone-model");
  });

  it("shows the last pick with no chat open", () => {
    const pick = { provider: "claude-code", model: "opus", reasoning_effort: "high" };
    const shown = agentWithModel(agent, null, providers, { workspace: pick });
    expect(shown.model_cfg).toEqual(pick);
    expect(shown.model.name).toBe("Opus 5.5");
  });

  it("finds a name through infoOf when the catalog lacks it", () => {
    const saved = { provider: "claude", model: "opus" };
    const shown = agentWithModel(agent, null, providers, { workspace: saved, infoOf: () => ({ id: "opus", name: "Claude Opus" }) });
    expect(shown.model.name).toBe("Claude Opus");
  });
});

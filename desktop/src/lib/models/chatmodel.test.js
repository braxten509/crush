import { describe, expect, it } from "vitest";
import { agentWithModel, shownChatModel } from "./chatmodel.js";

// The model a chat runs on, so a model picked in another chat never changes
// this one (owner, 2026-10-23).
const providers = [
  { id: "claude-code", models: [{ id: "opus", name: "Opus 5.5", context_window: 400_000 }] },
  { id: "codex-cli", models: [{ id: "gpt-6-sol", name: "GPT-6 Sol", context_window: 400_000 }] },
];
const agent = { is_ready: true, model: { id: "opus", name: "Opus 5.5", context_window: 400_000 }, model_cfg: { provider: "claude-code", model: "opus", reasoning_effort: "high" } };

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

  it("keeps the agent's model when the chat has none", () => {
    expect(agentWithModel(agent, { id: "s" }, providers)).toBe(agent);
    expect(agentWithModel(agent, null, providers)).toBe(agent);
    expect(agentWithModel(null, { model: { provider: "codex-cli", model: "gpt-6-sol" } }, providers)).toBe(null);
  });

  it("names a model this window has not read by its id, never the agent's", () => {
    const shown = agentWithModel(agent, { model: { provider: "gone", model: "gone-model" } }, providers);
    expect(shown.model_cfg).toEqual({ provider: "gone", model: "gone-model" });
    expect(shown.model.name).toBe("gone-model");
  });

  // The engine keeps its old model while another chat runs (UpdateModels
  // skips a busy agent), so a new chat shows the saved choice, as the menu
  // does (owner, 2026-10-24).
  it("shows the saved workspace model over a stale agent for a chat without one", () => {
    const stale = { is_ready: true, model: { id: "flash", name: "DeepSeek V4.1 Flash" }, model_cfg: { provider: "zen", model: "flash", reasoning_effort: "max" } };
    const saved = { provider: "claude-code", model: "opus", reasoning_effort: "high" };
    const shown = agentWithModel(stale, null, providers, { workspace: saved });
    expect(shown.model_cfg).toEqual(saved);
    expect(shown.model.name).toBe("Opus 5.5");
  });

  it("finds a name through infoOf when the catalog lacks it", () => {
    const saved = { provider: "claude", model: "opus" };
    const shown = agentWithModel(agent, null, providers, { workspace: saved, infoOf: () => ({ id: "opus", name: "Claude Opus" }) });
    expect(shown.model.name).toBe("Claude Opus");
  });
});

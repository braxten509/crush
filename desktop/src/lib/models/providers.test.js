import { describe, expect, test } from "vitest";
import { footerText } from "../timeline.js";
import { footerProviders, needsKeyFirst, safeProvider } from "./providers.js";
import { cheapestHostOn, qualityFloorOn } from "./openrouter.js";

const cfg = {
  providers: new Map([
    ["claude-code", { id: "claude-code", name: "Claude Code", models: [{ id: "claude-opus-5-5", name: "Opus 5.5" }], chatgpt_models: null, grok_models: null }],
    ["openai", { id: "openai", name: "", models: [], chatgpt_models: [{ id: "gpt-6-sol", name: "GPT-6 Sol" }], grok_models: null }],
  ]),
};
const catalog = [
  { id: "openai", name: "OpenAI", models: [{ id: "gpt-6-luna", name: "GPT-6 Luna" }] },
  { id: "anthropic", name: "Anthropic", models: [{ id: "claude-haiku-5-5", name: "Haiku 5.5" }] },
];
const finished = (provider, model) => ({
  id: "a1",
  role: "assistant",
  provider,
  model,
  parts: [{ type: "finish", data: { reason: "end_turn", time: 20 } }],
});

describe("footerProviders (the terminal's cfg.GetModel order)", () => {
  test("an agent CLI's model and name come from the config, not the catalog", () => {
    const list = footerProviders(cfg, catalog);
    expect(footerText(finished("claude-code", "claude-opus-5-5"), list, 11).text).toBe("Opus 5.5 via Claude Code in 9s");
  });

  test("account models, then the catalog's, and the catalog's name when the config has none", () => {
    const list = footerProviders(cfg, catalog);
    expect(footerText(finished("openai", "gpt-6-sol"), list, 20).text).toBe("GPT-6 Sol via OpenAI in 0s");
    expect(footerText(finished("openai", "gpt-6-luna"), list, 20).text).toBe("GPT-6 Luna via OpenAI in 0s");
  });

  test("catalog providers missing from the config still name their models; before the config loads, the catalog alone", () => {
    expect(footerText(finished("anthropic", "claude-haiku-5-5"), footerProviders(cfg, catalog), 20).text).toBe("Haiku 5.5 via Anthropic in 0s");
    expect(footerProviders(null, catalog)).toEqual(catalog);
  });
});

describe("the picker's provider view keeps the cheapest host switch (owner, 2026-10-22)", () => {
  test("turning it off survives the read the switch does next; a provider without it asks for the cheapest host", () => {
    const read = (saved) => ({ providers: new Map([["openrouter", safeProvider("openrouter", saved)]]) });
    expect(safeProvider("openrouter", { type: "openrouter", cheapest_host: false }).cheapest_host).toBe(false);
    expect(cheapestHostOn(read({ type: "openrouter", cheapest_host: false }), "openrouter")).toBe(false);
    expect(cheapestHostOn(read({ type: "openrouter", cheapest_host: true }), "openrouter")).toBe(true);
    expect(cheapestHostOn(read({ type: "openrouter" }), "openrouter")).toBe(true);
  });
});

// The FP8 floor switch sits under the cheapest host one and reads back the
// same way (owner, 2026-10-22).
describe("the picker's provider view keeps the FP8 floor switch (owner, 2026-10-22)", () => {
  test("turning it off survives the read the switch does next; a provider without it refuses hosts below FP8", () => {
    const read = (saved) => ({ providers: new Map([["openrouter", safeProvider("openrouter", saved)]]) });
    expect(safeProvider("openrouter", { type: "openrouter", quality_floor: false }).quality_floor).toBe(false);
    expect(qualityFloorOn(read({ type: "openrouter", quality_floor: false }), "openrouter")).toBe(false);
    expect(qualityFloorOn(read({ type: "openrouter", quality_floor: true }), "openrouter")).toBe(true);
    expect(qualityFloorOn(read({ type: "openrouter" }), "openrouter")).toBe(true);
  });
});

describe("needsKeyFirst (picking a model before its key is saved)", () => {
  test("the Codex API asks for its key until one is saved", () => {
    expect(needsKeyFirst("codex-api", safeProvider("codex-api", { type: "openai" }))).toBe(true);
    expect(needsKeyFirst("codex-api", safeProvider("codex-api", { type: "openai", has_api_key: true }))).toBe(false);
  });

  test("every Tinfoil account asks for its key until one is saved", () => {
    for (const id of ["tinfoil", "tinfoil-2"]) {
      expect(needsKeyFirst(id, safeProvider(id, { type: "openai-compat" }))).toBe(true);
      expect(needsKeyFirst(id, safeProvider(id, { type: "openai-compat", has_api_key: true }))).toBe(false);
    }
  });

  test("a keyless local provider is used as it is", () => {
    expect(needsKeyFirst("ollama", safeProvider("ollama", { type: "openai-compat" }))).toBe(false);
  });
});

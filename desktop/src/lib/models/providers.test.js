import { describe, expect, test } from "vitest";
import { footerText } from "../timeline.js";
import { flatModel, footerProviders, needsKeyFirst, safeProvider } from "./providers.js";
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

// A reply's footer names the model and the provider it ran on.
function expectFooter(text, model, provider) {
  expect(text).toContain(model);
  expect(text).toContain(provider);
}

describe("footerProviders (the terminal's cfg.GetModel order)", () => {
  test("an agent CLI's model and name come from the config, not the catalog", () => {
    const list = footerProviders(cfg, catalog);
    expectFooter(footerText(finished("claude-code", "claude-opus-5-5"), list, 11).text, "Opus 5.5", "Claude Code");
  });

  test("account models, then the catalog's, and the catalog's name when the config has none", () => {
    const list = footerProviders(cfg, catalog);
    expectFooter(footerText(finished("openai", "gpt-6-sol"), list, 20).text, "GPT-6 Sol", "OpenAI");
    expectFooter(footerText(finished("openai", "gpt-6-luna"), list, 20).text, "GPT-6 Luna", "OpenAI");
  });

  test("catalog providers missing from the config still name their models; before the config loads, the catalog alone", () => {
    expectFooter(footerText(finished("anthropic", "claude-haiku-5-5"), footerProviders(cfg, catalog), 20).text, "Haiku 5.5", "Anthropic");
    expect(footerProviders(null, catalog)).toEqual(catalog);
  });
});

// Engines from v0.100.0 on send catwalk v3 models, which nest what the
// screens read flat: a slip here silently drops the effort levels, the
// thinking switch and pictures.
describe("catwalk v3 models keep the thinking, effort and picture controls", () => {
  const v3 = {
    id: "claude-opus-5-5",
    name: "Opus 5.5",
    pricing: { input: 5, output: 25, cache_create: 6.25, cache_hit: 0.5 },
    context_window: 1_000_000,
    default_max_tokens: 64_000,
    reasoning: {
      thinking: "toggleable",
      effort_levels: [
        { value: "low", display: "Low" },
        { value: "high", display: "High" },
        { value: "xhigh", display: "X-High" },
      ],
      default_effort_level: "high",
    },
    capabilities: { vision: true, audio: false },
  };

  test("the nested fields give the flat names back, and stay", () => {
    const flat = flatModel(v3);
    expect(flat).toMatchObject({
      can_reason: true,
      reasoning_levels: ["low", "high", "xhigh"],
      default_reasoning_effort: "high",
      supports_attachments: true,
      cost_per_1m_in: 5,
      cost_per_1m_out: 25,
      cost_per_1m_in_cached: 6.25,
      cost_per_1m_out_cached: 0.5,
    });
    expect(flat.reasoning).toEqual(v3.reasoning);
    expect(v3.can_reason).toBeUndefined();
  });

  test("never and unset cannot reason, always can; no effort levels leaves none", () => {
    const never = flatModel({ id: "m", reasoning: { thinking: "never" }, capabilities: { vision: false, audio: false } });
    expect(never).toMatchObject({ can_reason: false, supports_attachments: false });
    expect("reasoning_levels" in never || "default_reasoning_effort" in never).toBe(false);
    expect(flatModel({ id: "m", reasoning: { thinking: "" } }).can_reason).toBe(false);
    const always = flatModel({ id: "r1", reasoning: { thinking: "always" } });
    expect(always.can_reason).toBe(true);
    expect("reasoning_levels" in always).toBe(false);
  });

  test("a model from an older engine stays as it is; its flat names win", () => {
    const old = { id: "opus", name: "Opus", can_reason: true, reasoning_levels: ["low", "high"], default_reasoning_effort: "low", supports_attachments: true, cost_per_1m_in: 5, cost_per_1m_out: 25 };
    expect(flatModel(old)).toEqual(old);
    expect(flatModel({ ...v3, supports_attachments: false, reasoning_levels: ["max"] })).toMatchObject({ supports_attachments: false, reasoning_levels: ["max"] });
  });

  test("the picker's view of a configured v3 model has every control", () => {
    expect(safeProvider("anthropic", { type: "anthropic", models: [v3] }).models).toEqual([
      { id: "claude-opus-5-5", name: "Opus 5.5", can_reason: true, reasoning_levels: ["low", "high", "xhigh"], default_reasoning_effort: "high", default_max_tokens: 64_000, supports_attachments: true, context_window: 1_000_000, reasoning: v3.reasoning },
    ]);
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

  test("every OrcaRouter account asks for its key until one is saved", () => {
    for (const id of ["orcarouter", "orcarouter-2"]) {
      expect(needsKeyFirst(id, safeProvider(id, { type: "openai-compat" }))).toBe(true);
      expect(needsKeyFirst(id, safeProvider(id, { type: "openai-compat", has_api_key: true }))).toBe(false);
    }
    expect(needsKeyFirst("work", safeProvider("work", { type: "openai-compat", base_host: "api.orcarouter.ai" }))).toBe(true);
  });

  test("a keyless local provider is used as it is", () => {
    expect(needsKeyFirst("ollama", safeProvider("ollama", { type: "openai-compat" }))).toBe(false);
  });
});

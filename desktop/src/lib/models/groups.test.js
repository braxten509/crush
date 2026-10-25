import { describe, expect, it } from "vitest";
import { buildGroups, filterQuery, filterValue, levelTitle, rememberedModel, selectedModelOf } from "./groups.js";
import { safeProvider } from "./providers.js";

// The model picker's groups (desktop-design-5-6 2.3): a slip shows the wrong
// models, marks the wrong one chosen, or saves a model that does not exist.
const catalog = [
  { id: "openai", name: "OpenAI", models: [{ id: "gpt", name: "GPT" }] },
  { id: "anthropic", name: "Anthropic", models: [{ id: "opus", name: "Opus" }, { id: "sonnet", name: "Sonnet" }] },
  { id: "off", name: "Off", models: [{ id: "m", name: "M" }] },
];

function cfg(over = {}) {
  return {
    models: {},
    // As the app reads them: through safeProvider, as readModelConfig does.
    providers: new Map(
      Object.entries({
        zeta: { name: "Zeta", type: "openai-compat", models: [{ id: "z1" }] },
        alpha: { name: "Alpha", models: [{ id: "a1", name: "A One" }] },
        anthropic: { name: "Claude", models: [{ id: "opus", name: "Opus 5" }, { id: "extra" }] },
        off: { disable: true },
        openai: { has_oauth: true, chatgpt_models: [{ id: "chat-only", name: "Chat Only" }] },
      }).map(([id, p]) => [id, safeProvider(id, p)]),
    ),
    ...over,
  };
}

describe("buildGroups", () => {
  it("orders custom providers by name, then the catalog, skipping disabled ones", () => {
    const { items } = buildGroups({ catalog, cfg: cfg(), type: "large" });
    expect([...new Set(items.map((i) => i.group))]).toEqual(["Alpha", "Zeta", "OpenAI", "Claude"]);
  });

  it("uses configured names and models, and a sign-in's own models", () => {
    const { items } = buildGroups({ catalog, cfg: cfg(), type: "large" });
    const claude = items.filter((i) => i.provider.id === "anthropic").map((i) => i.model.name);
    expect(claude).toEqual(["Opus 5", "Sonnet", "extra"]);
    expect(items.filter((i) => i.provider.id === "openai").map((i) => i.model.id)).toEqual(["chat-only"]);
  });

  it("marks the chosen model, preferring its Recently used row, and drops stale recents", () => {
    const recent = [{ provider: "anthropic", model: "sonnet" }, { provider: "gone", model: "x" }];
    const { items, selectedKey, prunedRecent } = buildGroups({ catalog, cfg: cfg({ recent_models: { large: recent } }), type: "large", current: { provider: "anthropic", model: "sonnet" } });
    expect(items[0].group).toBe("Recently used");
    expect(selectedKey).toBe(items[0].id);
    expect(prunedRecent).toEqual([{ provider: "anthropic", model: "sonnet" }]);
    // Nothing dropped: nothing to save.
    expect(buildGroups({ catalog, cfg: cfg(), type: "large" }).prunedRecent).toBeNull();
  });
});

describe("row helpers", () => {
  it("filters on group and model name without spaces or case", () => {
    const item = { group: "Open AI", model: { name: "GPT 5" } };
    expect(filterValue(item)).toBe("openaigpt5");
    expect(filterValue(item).includes(filterQuery("AI g"))).toBe(true);
  });

  it("chooses a model with its defaults, and titles effort levels", () => {
    const item = { provider: { id: "p" }, model: { id: "m", default_reasoning_effort: "high", default_max_tokens: 8 } };
    expect(selectedModelOf(item)).toEqual({ model: "m", provider: "p", reasoning_effort: "high", max_tokens: 8 });
    expect(levelTitle("xhigh")).toBe("X-High");
    expect(levelTitle("extra_low")).toBe("Extra Low");
  });

  it("chooses a model with the effort last used with it while the model still has it (owner, 2026-10-22)", () => {
    const model = { id: "m", reasoning_levels: ["low", "high", "max"], default_reasoning_effort: "high", default_max_tokens: 8 };
    const item = { provider: { id: "p" }, model };
    expect(selectedModelOf(item, { "p:m": "max" })).toEqual({ model: "m", provider: "p", reasoning_effort: "max", max_tokens: 8 });
    expect(selectedModelOf(item, { "p:m": "ultra" }).reasoning_effort).toBe("high");
    expect(selectedModelOf(item, { "p:other": "low" }).reasoning_effort).toBe("high");
  });

  it("brings a chat's model back with its remembered effort", () => {
    const config = {
      providers: { p: { models: [{ id: "m", reasoning_levels: ["low", "high", "max"], default_reasoning_effort: "high", default_max_tokens: 8 }] } },
      model_efforts: { "p:m": "max" },
    };
    expect(rememberedModel(config, "p", "m")).toEqual({ model: "m", provider: "p", reasoning_effort: "max", max_tokens: 8 });
    expect(rememberedModel(config, "p", "gone")).toEqual({ model: "gone", provider: "p" });
  });
});

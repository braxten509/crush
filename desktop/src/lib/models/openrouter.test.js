import { describe, expect, it } from "vitest";
import { addableProviders, catalogRows, contextText, priceText, savedWith, savedWithout } from "./openrouter.js";

// The OpenRouter "+" (owner, 2026-10-22): a slip lists a model twice, drops
// a saved one, or shows the wrong price.
const kimi = { id: "moonshot/kimi-k3", name: "Moonshot: Kimi K3", cost_per_1m_in: 0.6, cost_per_1m_out: 2.5, context_window: 262144 };
const glm = { id: "z-ai/glm-5.3", name: "Z.ai: GLM 5.3", cost_per_1m_in: 0.5, cost_per_1m_out: 1.9, context_window: 200000 };
const free = { id: "free/coder:free", name: "Free Coder", context_window: 1048576 };

describe("OpenRouter models", () => {
  it("finds the OpenRouter providers that take saved models", () => {
    const cfg = { providers: new Map([
      ["openrouter", { id: "openrouter", name: "OpenRouter", type: "openrouter", models: [] }],
      ["off", { id: "off", name: "Off", type: "openrouter", disable: true, models: [] }],
      ["deepinfra", { id: "deepinfra", name: "DeepInfra", type: "openai-compat", models: [] }],
    ]) };
    expect(addableProviders(cfg)).toEqual([{ id: "openrouter", name: "OpenRouter" }]);
  });

  it("searches names and ids, spaces and case ignored", () => {
    expect(catalogRows([kimi, glm, free], "").length).toBe(3);
    expect(catalogRows([kimi, glm, free], "KIMI k3").map((m) => m.id)).toEqual(["moonshot/kimi-k3"]);
    expect(catalogRows([kimi, glm, free], "z-ai").map((m) => m.id)).toEqual(["z-ai/glm-5.3"]);
  });

  it("writes prices per million and the context size", () => {
    expect(priceText(kimi)).toBe("$0.6 / $2.5");
    expect(priceText(free)).toBe("free");
    expect(priceText({ cost_per_1m_in: 12, cost_per_1m_out: 0.125 })).toBe("$12 / $0.13");
    expect(contextText(262144)).toBe("262K");
    expect(contextText(1048576)).toBe("1M");
    expect(contextText(2000000)).toBe("2M");
    expect(contextText(0)).toBe("");
  });

  it("adds picked models after the saved ones once, and removes one", () => {
    expect(savedWith([kimi], [glm, kimi]).map((m) => m.id)).toEqual(["moonshot/kimi-k3", "z-ai/glm-5.3"]);
    expect(savedWith(undefined, [glm]).map((m) => m.id)).toEqual(["z-ai/glm-5.3"]);
    expect(savedWithout([kimi, glm], "moonshot/kimi-k3").map((m) => m.id)).toEqual(["z-ai/glm-5.3"]);
  });
});

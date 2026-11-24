import { describe, expect, it } from "vitest";
import { helperAgent } from "./helpers.svelte.js";

// A sub-agent's panel shows its own provider, not the parent's.
describe("helperAgent", () => {
  it("matches an agent CLI by its type", () => {
    const { agent, kind } = helperAgent({ cli: "claude", model: "opus" }, [{ id: "claude", type: "claude-code", models: [{ id: "opus", name: "Claude Opus" }] }]);
    expect(agent.model_cfg.provider).toBe("claude");
    expect(agent.model.name).toBe("Claude Opus");
    expect(kind).toBe("claude-code");
  });
});

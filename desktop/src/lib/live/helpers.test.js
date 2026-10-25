import { describe, expect, it } from "vitest";
import { limitKind } from "../footer.js";
import { helperAgent } from "./helpers.svelte.js";

// A sub-agent's panel shows its own provider's usage, not the parent's
// (owner, 2026-10-24: a Kimi sub-agent showed no meter).
describe("helperAgent", () => {
  it("keeps an API provider's id when the catalog does not list it, so its meter shows", () => {
    const { agent } = helperAgent({ cli: "kimi-coding", model: "k3.1-pro", effort: "high" }, [{ id: "claude", type: "claude-code", models: [] }]);
    expect(agent.model_cfg.provider).toBe("kimi-coding");
    expect(limitKind(agent, [])).toBe("kimi-coding");
  });

  it("matches an agent CLI by its type", () => {
    const { agent, kind } = helperAgent({ cli: "claude", model: "opus" }, [{ id: "claude", type: "claude-code", models: [{ id: "opus", name: "Claude Opus" }] }]);
    expect(agent.model_cfg.provider).toBe("claude");
    expect(agent.model.name).toBe("Claude Opus");
    expect(kind).toBe("claude-code");
  });
});

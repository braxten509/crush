import { describe, expect, it } from "vitest";
import { safeProvider } from "../models/providers.js";
import { accountGroups } from "./providers.js";

const provider = (id, fields) => [id, safeProvider(id, fields)];

const cfg = {
  providers: new Map([
    provider("claude-code", { name: "Claude Code", type: "claude-code" }),
    provider("codex-cli", { name: "Codex", type: "codex-cli" }),
    provider("grok-cli", { name: "Grok", type: "grok-cli" }),
    provider("agy-cli", { name: "Antigravity", type: "agy-cli", disable: true }),
    provider("anthropic", { name: "Anthropic", type: "anthropic", has_api_key: true }),
    provider("my-proxy", { name: "My Proxy", type: "openai", has_api_key: true }),
    provider("openrouter", { name: "OpenRouter", type: "openrouter", disable: true }),
  ]),
};

const catalog = [
  { id: "anthropic", name: "Anthropic", type: "anthropic", models: [] },
  { id: "openrouter", name: "OpenRouter", type: "openrouter", models: [] },
  { id: "deepinfra", name: "DeepInfra", type: "openai", models: [] },
  { id: "hyper", name: "Hyper", type: "hyper", models: [] },
  { id: "copilot", name: "GitHub Copilot", type: "copilot", models: [] },
  { id: "openai", name: "ChatGPT", type: "openai", models: [] },
];

describe("accountGroups", () => {
  it("lists the installed CLIs first, then every API provider", () => {
    const groups = accountGroups(cfg, catalog);
    expect(groups.map((group) => group.label)).toEqual(["Agent CLIs", "API providers"]);
    // A CLI hidden in the config is left out.
    expect(groups[0].rows.map((row) => row.id)).toEqual(["claude-code", "codex-cli", "grok-cli"]);
    // Custom providers first, then the catalog's order; the three removed
    // platforms and a disabled provider are not listed.
    expect(groups[1].rows.map((row) => row.id)).toEqual(["my-proxy", "anthropic", "deepinfra"]);
    expect(groups[1].rows[0].name).toBe("My Proxy");
    expect(groups[1].rows[2].configured).toBe(false);
    expect(groups[1].rows[1].hasKey).toBe(true);
  });

  it("keeps a provider's own name", () => {
    const renamed = { providers: new Map([provider("anthropic", { name: "Work Anthropic", type: "anthropic" })]) };
    const groups = accountGroups(renamed, catalog);
    expect(groups[1].rows.find((row) => row.id === "anthropic").name).toBe("Work Anthropic");
  });
});

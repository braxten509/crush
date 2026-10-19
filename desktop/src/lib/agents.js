// The AI tool behind a chat, by name, for the chat list's agent line (O1,
// decided at PLAN.md:299). A chat's context_budget.provider is the provider
// type the CLI run recorded (internal/agent/cli_steps.go:402-404); a built-in
// CLI provider's id is that same type (internal/config/cli_providers.go:39-83),
// so the provider of a loaded chat's last model maps the same way.

// Short names, as the design's "claude-code" → "Claude": the providers' names
// of internal/config/cli_providers.go:39-83, with OpenCode named as its CLI
// (internal/cliupdate/cliupdate.go:48), not its "Go" plan.
export const AGENT_NAMES = {
  "claude-code": "Claude",
  "codex-cli": "Codex",
  "grok-cli": "Grok",
  "agy-cli": "Antigravity",
  "opencode-cli": "OpenCode",
};

// An agent CLI's name; otherwise the provider's name from the providers list
// (GET providers), else the provider id itself. "" when there is no provider.
export function agentName(provider, providers = []) {
  if (!provider) return "";
  if (Object.hasOwn(AGENT_NAMES, provider)) return AGENT_NAMES[provider];
  const known = providers.find((p) => p?.id === provider);
  return known?.name || provider;
}

// A chat's agent: its context budget's provider, else the last loaded model's
// provider ({provider, model} or null); "" (no agent line) with neither.
export function chatAgent(session, lastModel = null, providers = []) {
  return agentName(session?.context_budget?.provider || lastModel?.provider || "", providers);
}

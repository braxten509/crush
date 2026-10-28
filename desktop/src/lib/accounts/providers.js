// Settings → Accounts' sidebar (owner, 2026-10-24): the agent CLIs installed
// here, then every API provider the model picker offers. Configured providers
// come first (the model picker's custom ones by name, then the catalog's in
// its order), so a key can be added for any provider the picker shows.
import { AGENT_CLIS, cliOf } from "./clis.js";

// The three platforms removed from Accounts (owner, 2026-10-24): their
// sign-in stays in the terminal (`crush login hyper|copilot|openai`).
const REMOVED = new Set(["hyper", "copilot", "openai"]);

// row is one sidebar entry and its detail's data.
function row(id, name, provider, extra = {}) {
  return {
    id,
    name,
    type: provider?.type ?? "",
    configured: Boolean(provider),
    hasKey: provider?.has_api_key === true,
    hasOAuth: provider?.has_oauth === true,
    oauthExpired: provider?.oauth_expired === true,
    flatRate: provider?.flat_rate === true,
    ...extra,
  };
}

// → [{label, rows}] with "Agent CLIs" first, then "Subscriptions" (paid
// plans like Kimi Coding, flat_rate in the config; owner, 2026-10-24), then
// "API providers".
export function accountGroups(cfg, catalog) {
  const providers = cfg?.providers ?? new Map();
  const list = Array.isArray(catalog) ? catalog : [];

  const clis = [];
  for (const cli of AGENT_CLIS) {
    const provider = providers.get(cli.kind);
    if (!provider || provider.disable) continue; // not installed, or hidden
    clis.push(row(cli.kind, provider.name || cli.name, provider, { cli }));
  }

  // Custom providers (the picker's own additions, a second API account among
  // them) by name, then the catalog's in its order.
  const known = new Set(list.map((entry) => entry.id));
  const custom = [];
  for (const [id, provider] of providers) {
    if (provider.disable || known.has(id) || cliOf(id) || REMOVED.has(id)) continue;
    custom.push(row(id, provider.name || id, provider));
  }
  custom.sort((a, b) => a.name.localeCompare(b.name));

  const api = [];
  for (const entry of list) {
    if (REMOVED.has(entry.id) || cliOf(entry.id)) continue;
    const provider = providers.get(entry.id);
    if (provider?.disable) continue;
    api.push(row(entry.id, provider?.name || entry.name || entry.id, provider));
  }

  const plans = [];
  const perUse = [];
  for (const entry of [...custom, ...api]) (entry.flatRate ? plans : perUse).push(entry);

  return [
    { label: "Agent CLIs", rows: clis },
    { label: "Subscriptions", rows: plans },
    { label: "API providers", rows: perUse },
  ];
}

// The label of a CLI that signs in itself (Antigravity, OpenCode Go and
// Zen) once signed in (owner, 2026-10-28): the email its login uses when
// the CLI keeps one, else the tool's name, as Claude Code and Codex rows
// show; "Sign-in" until then.
export function cliRowLabel(row, signedIn, email) {
  if (!signedIn) return "Sign-in";
  return email || row.name;
}

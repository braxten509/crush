// The token limit the CONTEXT meter measures against (SIDE-015), a port of
// compactionLimit (internal/ui/model/composer_footer.go:183-207) and the
// config rules it uses (internal/config/autocompact.go:11-41).
//
//   compactionLimit({agent, session, providers, options})
//     agent      AgentInfo (ws.agent): its model (catwalk) and model_cfg
//     session    the shown chat's Session (its context_budget), or null
//     providers  ws.providers: a provider's type by its id
//     options    ws.config.options (auto_compact_token_limit)
//   → tokens; 0 means unknown ("Context unknown"): an agent CLI that keeps
//     its own context reports its budget, and a missing report is not a
//     reason to fall back to Crush's own limit.

export const DEFAULT_AUTO_COMPACT_TOKEN_LIMIT = 400_000;

// The agent CLIs (internal/config/cli_providers.go:14-19); all five keep
// their own context (autocompact.go:11-18).
export const CLI_KINDS = new Set(["claude-code", "codex-cli", "grok-cli", "agy-cli", "opencode-cli"]);

export function nativeCompaction(kind) {
  return CLI_KINDS.has(kind);
}

// The smaller of the configured limit and the window minus a buffer: a fifth
// of the window, or 20,000 above 200,000 tokens. No window: the configured.
export function fallbackCompactionLimit(window, configured) {
  if (!(window > 0)) return configured;
  const buffer = window > 200_000 ? 20_000 : Math.trunc(window / 5);
  return Math.min(configured, window - buffer);
}

// Options.GetAutoCompactTokenLimit: the saved threshold, else the default.
export function autoCompactTokenLimit(options) {
  const limit = Number(options?.auto_compact_token_limit);
  return limit > 0 ? limit : DEFAULT_AUTO_COMPACT_TOKEN_LIMIT;
}

// The provider's type. The terminal reads it from the config's providers,
// which never reach the window (pickConfig drops them), so it comes from
// GET providers; a provider that list lacks keeps its id as its type, as the
// terminal does, and the agent CLIs' ids are their types.
export function providerKind(providerId, providers = []) {
  const found = (Array.isArray(providers) ? providers : []).find((p) => p?.id === providerId);
  return found?.type || providerId || "";
}

export function compactionLimit({ agent = null, session = null, providers = [], options = undefined } = {}) {
  const fallback = autoCompactTokenLimit(options);
  const model = agent?.model_cfg ? agent : null;
  const kind = model ? providerKind(model.model_cfg.provider, providers) : "";
  if (nativeCompaction(kind)) {
    const budget = session?.context_budget;
    if (budget && budget.provider === kind && budget.model === model.model_cfg.model) return Number(budget.tokens) || 0;
    return 0;
  }
  if (model) return fallbackCompactionLimit(Number(model.model?.context_window) || 0, fallback);
  return fallback;
}

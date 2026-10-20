// The configured providers as the model picker and sign-in sheets need them
// (desktop-design-5-6 2.3, 2.4). The Rust side cuts every credential out of
// GET …/config before the answer reaches the page (commands.rs
// redact_answer): a provider comes with has_api_key, has_oauth and the
// token's expiry times (oauth_times) instead of the key and the token.
import { api } from "../engine/api.js";

const MODEL_FIELDS = ["id", "name", "can_reason", "reasoning_levels", "default_reasoning_effort", "default_max_tokens", "supports_attachments", "context_window"];

function pickModel(model) {
  const out = {};
  for (const key of MODEL_FIELDS) if (model?.[key] !== undefined) out[key] = structuredClone(model[key]);
  return out;
}

const pickModels = (list) => (Array.isArray(list) ? list.filter((m) => m && typeof m === "object").map(pickModel) : null);

// oauth.Token.IsExpired (internal/oauth/token.go:69-72): expired a tenth of
// its lifetime (at least 30 s) before expires_at, unix seconds.
function oauthExpired(token) {
  const at = Number(token?.expires_at ?? 0);
  const buffer = Math.max(Math.floor(Number(token?.expires_in ?? 0) / 10), 30);
  return Math.floor(Date.now() / 1000) >= at - buffer;
}

// One configured provider without its secrets.
export function safeProvider(id, cfg) {
  return {
    id,
    name: typeof cfg?.name === "string" ? cfg.name : "",
    type: typeof cfg?.type === "string" ? cfg.type : "",
    disable: cfg?.disable === true,
    has_api_key: cfg?.has_api_key === true,
    has_oauth: cfg?.has_oauth === true,
    oauth_expired: cfg?.has_oauth === true && oauthExpired(cfg.oauth_times),
    models: pickModels(cfg?.models) ?? [],
    chatgpt_models: pickModels(cfg?.chatgpt_models),
    grok_models: pickModels(cfg?.grok_models),
  };
}

// {models, recent_models, coderType, providers: Map(id → safe provider)}
// from GET …/config; the raw answer is dropped here.
export async function readModelConfig() {
  const raw = await api.config();
  const providers = new Map();
  const list = raw?.providers;
  if (list && typeof list === "object") {
    for (const [id, cfg] of Object.entries(list)) providers.set(id, safeProvider(id, cfg));
  }
  return {
    models: structuredClone(raw?.models ?? {}),
    recent_models: structuredClone(raw?.recent_models ?? {}),
    coderType: raw?.agents?.coder?.model === "small" ? "small" : "large",
    providers,
  };
}

// The catalog (GET …/providers), keys and headers dropped.
export async function readCatalog() {
  const list = (await api.providers()) ?? [];
  return list.map((p) => ({
    id: p.id,
    name: p.name ?? "",
    type: p.type ?? "",
    default_large_model_id: p.default_large_model_id ?? "",
    default_small_model_id: p.default_small_model_id ?? "",
    models: pickModels(p.models) ?? [],
  }));
}

// The providers a reply's footer and author read, as the terminal's
// cfg.GetModel and cfg.Providers.Get do (internal/config/config.go:168-186):
// every configured provider first, the agent CLIs among them (owner,
// 2026-10-20: footers said "Unknown Model via claude-code" because the
// catalog has no agent CLIs), with its models, ChatGPT and Grok models, and
// the catalog's models after them; then the catalog providers the config
// does not have. [{id, name, models}]
export function footerProviders(cfg, catalog = []) {
  const out = [];
  const seen = new Set();
  const list = Array.isArray(catalog) ? catalog : [];
  for (const [id, p] of cfg?.providers ?? []) {
    seen.add(id);
    const fromCatalog = list.find((c) => c?.id === id);
    out.push({
      id,
      name: p?.name || fromCatalog?.name || "",
      models: [...(p?.models ?? []), ...(p?.chatgpt_models ?? []), ...(p?.grok_models ?? []), ...(fromCatalog?.models ?? [])],
    });
  }
  for (const c of list) if (c && !seen.has(c.id)) out.push(c);
  return out;
}

// The catalog model for a selected model, configured names first.
export function modelInfo(cfg, catalog, selected) {
  if (!selected?.provider || !selected?.model) return null;
  const configured = cfg?.providers.get(selected.provider);
  const fromConfig = configured?.models.find((m) => m.id === selected.model);
  const fromAccount = [...(configured?.chatgpt_models ?? []), ...(configured?.grok_models ?? [])].find((m) => m.id === selected.model);
  const fromCatalog = catalog.find((p) => p.id === selected.provider)?.models.find((m) => m.id === selected.model);
  if (!fromConfig && !fromCatalog && !fromAccount) return null;
  return { ...(fromCatalog ?? {}), ...(fromAccount ?? {}), ...(fromConfig ?? {}), name: fromConfig?.name || fromAccount?.name || fromCatalog?.name || selected.model };
}

// The configured providers as the model picker and sign-in sheets need them
// (desktop-design-5-6 2.3, 2.4). The Rust side cuts every credential out of
// GET …/config before the answer reaches the page (commands.rs
// redact_answer): a provider comes with has_api_key, has_oauth and the
// token's expiry times (oauth_times) instead of the key and the token.
import { api } from "../engine/api.js";

// reasoning: catwalk v3's own, kept for its thinking word ("always" has no
// thinking switch, footer.js supportsThinkingToggle).
const MODEL_FIELDS = ["id", "name", "can_reason", "reasoning_levels", "default_reasoning_effort", "default_max_tokens", "supports_attachments", "context_window", "reasoning"];

const PRICES = [
  ["cost_per_1m_in", "input"],
  ["cost_per_1m_out", "output"],
  ["cost_per_1m_in_cached", "cache_create"],
  ["cost_per_1m_out_cached", "cache_hit"],
];

// A catwalk model with the flat names the screens read. Engines from v0.100.0
// on send catwalk v3, which nests them: reasoning {thinking, effort_levels
// [{value, display}], default_effort_level}, capabilities {vision} and
// pricing {input, output, cache_create, cache_hit}
// (catwalk/v3@v3.0.1 pkg/catwalk/provider.go:91-172). Each nested one gives
// its old name back as the engine's own config migration maps them
// (internal/config/load.go migrateCatwalkV2Model): thinking "always" or
// "toggleable" is can_reason (catwalk's CanReason), vision is
// supports_attachments. A flat name the model already has (an engine from
// before v0.100.0, still running after an update) stays as it is.
export function flatModel(model) {
  if (!model || typeof model !== "object") return model;
  const out = { ...model };
  const reasoning = model.reasoning;
  if (reasoning && typeof reasoning === "object") {
    const thinking = String(reasoning.thinking ?? "");
    if (out.can_reason === undefined) out.can_reason = thinking !== "" && thinking !== "never";
    const levels = (Array.isArray(reasoning.effort_levels) ? reasoning.effort_levels : [])
      .map((level) => (typeof level === "string" ? level : level?.value))
      .filter((value) => typeof value === "string" && value !== "");
    if (out.reasoning_levels === undefined && levels.length > 0) out.reasoning_levels = levels;
    const effort = reasoning.default_effort_level;
    if (out.default_reasoning_effort === undefined && typeof effort === "string" && effort !== "") out.default_reasoning_effort = effort;
  }
  const vision = model.capabilities?.vision;
  if (out.supports_attachments === undefined && typeof vision === "boolean") out.supports_attachments = vision;
  const pricing = model.pricing;
  if (pricing && typeof pricing === "object") {
    for (const [flat, nested] of PRICES) if (out[flat] === undefined && pricing[nested] !== undefined) out[flat] = pricing[nested];
  }
  return out;
}

function pickModel(model) {
  const flat = flatModel(model);
  const out = {};
  for (const key of MODEL_FIELDS) if (flat?.[key] !== undefined) out[key] = structuredClone(flat[key]);
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
    // OpenRouter: the model menu's "Cheapest host" switch reads this back
    // after saving, so it must survive the cut (owner, 2026-10-22: it
    // flipped straight back on). The "FP8 floor" switch reads its own that
    // way too.
    cheapest_host: cfg?.cheapest_host !== false,
    quality_floor: cfg?.quality_floor !== false,
    // A paid plan (Kimi Coding), not billed per use: Accounts lists it
    // under Subscriptions, not API providers.
    flat_rate: cfg?.flat_rate === true,
    // The base URL's host alone (commands.rs redact_config), so an
    // OrcaRouter account is known however it is named.
    base_host: typeof cfg?.base_host === "string" ? cfg.base_host : "",
    has_api_key: cfg?.has_api_key === true,
    has_oauth: cfg?.has_oauth === true,
    oauth_expired: cfg?.has_oauth === true && oauthExpired(cfg.oauth_times),
    models: pickModels(cfg?.models) ?? [],
    chatgpt_models: pickModels(cfg?.chatgpt_models),
    grok_models: pickModels(cfg?.grok_models),
  };
}

// Providers that are always listed but useless without a key (the engine
// adds the Codex API, Tinfoil and OrcaRouter with none, so Settings →
// Accounts can take it): picking one of their models opens the key sheet
// first. Another account of one ("tinfoil-2") is the same.
const KEY_FIRST = new Set(["codex-api", "tinfoil", "orcarouter"]);

const keyFirstProvider = (providerId) => KEY_FIRST.has(String(providerId ?? "").replace(/-\d+$/, ""));

// An OrcaRouter account, as the engine's config.IsOrcaRouterProvider names
// it: "orcarouter", a copy of it ("orcarouter-2", "orcarouter-2-2"), or any
// provider whose address is on orcarouter.ai.
export function isOrcaRouter(id, provider) {
  if (/^orcarouter(-\d+)*$/.test(String(id ?? ""))) return true;
  const host = String(provider?.base_host ?? "");
  return host === "orcarouter.ai" || host.endsWith(".orcarouter.ai");
}

export const needsKeyFirst = (providerId, configured) =>
  (keyFirstProvider(providerId) || isOrcaRouter(providerId, configured)) && !configured?.has_api_key && !configured?.has_oauth;

// {models, recent_models, providers: Map(id → safe provider)}
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
    model_efforts: structuredClone(raw?.model_efforts ?? {}),
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

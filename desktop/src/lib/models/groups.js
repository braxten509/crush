// The model picker's groups (desktop-design-5-6 2.3 rows 1-6; a port of
// internal/ui/dialog/models/models.go:350-560): custom providers first, by
// name then id; then the catalog's providers in catalog order, skipping
// disabled ones, a configured one with its configured name and models, a
// ChatGPT or Grok sign-in with the account's models; "Recently used" on top,
// its stale entries dropped (and the pruned list returned for saving).

const OPENAI = "openai";
const XAI = "xai";

// modelKey (models.go:597-602).
export function modelKey(providerId, modelId) {
  if (!providerId || !modelId) return "";
  return `${providerId}:${modelId}`;
}

// withConfiguredModels (models.go:509-533).
function withConfiguredModels(provider, configured) {
  const models = provider.models.map((m) => ({ ...m }));
  const index = new Map(models.map((m, i) => [m.id, i]));
  for (const model of configured.models) {
    if (!model.id) continue;
    if (index.has(model.id)) {
      if (model.name) models[index.get(model.id)].name = model.name;
      continue;
    }
    models.push({ ...model, name: model.name || model.id });
    index.set(model.id, models.length - 1);
  }
  return { ...provider, name: configured.name || provider.name, models };
}

// subscriptionModels (models.go:535-549).
function subscriptionModels(providerId, configured) {
  if (!configured?.has_oauth) return null;
  if (providerId === OPENAI) return configured.chatgpt_models ?? [];
  if (providerId === XAI) return configured.grok_models ?? [];
  return null;
}

// → {items: [{key, id, group, configured, recent, provider: {id, name, type},
// model}], selectedKey, prunedRecent (null when nothing was dropped)}.
// `type` is "large" or "small".
export function buildGroups({ catalog, cfg, type }) {
  const current = cfg.models?.[type] ?? {};
  const items = [];
  const byKey = new Map();
  let selectedKey = "";

  function group(name, configured, provider, models) {
    for (const model of models) {
      const key = modelKey(provider.id, model.id);
      const item = { key, id: `${name}\u0000${key}`, group: name, configured, recent: false, provider: { id: provider.id, name: provider.name || provider.id, type: provider.type ?? "" }, model };
      items.push(item);
      byKey.set(key, item);
      if (model.id === current.model && provider.id === current.provider) selectedKey = item.id;
    }
  }

  // addCustomProviderGroups (models.go:430-470).
  const known = new Set(catalog.map((p) => p.id));
  const custom = [];
  for (const [id, p] of cfg.providers) {
    if (p.disable) continue;
    if (!known.has(id)) custom.push({ id, name: p.name || id, p });
  }
  custom.sort((a, b) => (a.name < b.name ? -1 : a.name > b.name ? 1 : a.id < b.id ? -1 : a.id > b.id ? 1 : 0));
  const added = new Set();
  for (const c of custom) {
    added.add(c.id);
    group(c.name, true, { id: c.id, name: c.name, type: c.p.type }, c.p.models);
  }

  // addKnownProviderGroups (models.go:472-507).
  for (const provider of catalog) {
    if (added.has(provider.id)) continue;
    const configured = cfg.providers.get(provider.id);
    if (configured?.disable) continue;
    const shown = configured ? withConfiguredModels(provider, configured) : provider;
    const name = shown.name || provider.id;
    const account = subscriptionModels(provider.id, configured);
    if (account) {
      group(name, true, provider, account);
      continue;
    }
    group(name, Boolean(configured), provider, shown.models);
  }

  // addRecentGroup (models.go:551-595).
  const recentList = Array.isArray(cfg.recent_models?.[type]) ? cfg.recent_models[type] : [];
  const valid = [];
  const recentItems = [];
  for (const recent of recentList) {
    const item = byKey.get(modelKey(recent?.provider, recent?.model));
    if (!item) continue;
    valid.push(recent);
    const copy = { ...item, id: `\u0001recent\u0000${item.key}`, group: "Recently used", configured: false, recent: true };
    recentItems.push(copy);
    if (recent.model === current.model && recent.provider === current.provider) selectedKey = copy.id;
  }
  return {
    items: [...recentItems, ...items],
    selectedKey,
    prunedRecent: valid.length !== recentList.length ? valid : null,
  };
}

// The filter (internal/ui/dialog/models/list.go:175): group name and model
// name, spaces and case ignored.
export const filterValue = (item) => `${item.group}${item.model.name || item.model.id}`.replace(/\s+/g, "").toLowerCase();
export const filterQuery = (query) => String(query ?? "").replace(/\s+/g, "").toLowerCase();

// The SelectedModel a row chooses (item.go:91-98).
export function selectedModelOf(item) {
  const out = { model: item.model.id, provider: item.provider.id };
  if (item.model.default_reasoning_effort) out.reasoning_effort = item.model.default_reasoning_effort;
  if (item.model.default_max_tokens) out.max_tokens = item.model.default_max_tokens;
  return out;
}

// Title Case for effort levels ("xhigh" shows "X-High", reasoning.go:270).
export function levelTitle(level) {
  const raw = String(level ?? "");
  if (raw.toLowerCase() === "xhigh") return "X-High";
  return raw
    .split(/[-_ ]+/)
    .map((word) => (word ? word[0].toUpperCase() + word.slice(1) : word))
    .join(" ");
}

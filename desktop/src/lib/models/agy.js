// AGY selects effort by model ID. Keep those IDs for saved chats and ACP,
// and present the available variants as a single choice in the desktop picker.
const levels = ["low", "medium", "high", "xhigh", "max"];

export function agyFamilies(models) {
  const families = new Map();
  for (const model of models) {
    const at = model.id?.lastIndexOf("-") ?? -1;
    if (at < 1) continue;
    const effort = model.id.slice(at + 1);
    if (!levels.includes(effort)) continue;
    const base = model.id.slice(0, at);
    if (!families.has(base)) families.set(base, {});
    families.get(base)[effort] = model.id;
  }
  const variants = new Map();
  const ids = new Set(models.map((model) => model.id));
  for (const [base, family] of families) {
    const available = levels.filter((level) => family[level]);
    if (ids.has(base)) {
      family.default = base;
      available.unshift("default");
    }
    if (available.length < 2) continue;
    for (const level of available) {
      variants.set(family[level], { agyVariants: family, reasoning_levels: available, default_reasoning_effort: level, can_reason: true });
    }
  }
  return variants;
}

export function agyModel(model, variants) {
  const info = variants.get(model.id);
  if (!info) return model;
  const name = (model.name || model.id).replace(/ \((?:low|medium|high|xhigh|extra|max)\)$/i, "");
  return { ...model, ...info, name };
}

// Keep the active variant, then the most recently picked one, then AGY's
// first entry. Preserve the order in which model families appeared.
export function agyRows(items, current, recents) {
  const rows = new Map();
  for (const item of items) {
    const family = item.model.agyVariants;
    const key = family ?? item.id;
    if (!rows.has(key)) rows.set(key, item);
    if (!family) continue;
    const preferred = [current, ...recents].find((pick) => pick?.provider === item.provider.id && Object.values(family).includes(pick.model));
    if (preferred?.model === item.model.id) rows.set(key, item);
  }
  return [...rows.values()];
}

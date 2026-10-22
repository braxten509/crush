// The model menu over the model button (owner, 2026-10-19: picker "B"): the
// agents and providers in a column on the left, the chosen one's models on
// the right, effort under them. Built from lib/models/groups.js's rows.
export const RECENT = "Recently used";

// [{name, configured, items, addTo?}] in the rows' order. `addable` are the
// providers that take models saved from a catalog (lib/models/openrouter.js):
// each shows even with nothing saved yet, among the others by name, and its
// group's addTo names it for the "+ Add model" row.
export function menuGroups(items, addable = []) {
  const groups = [];
  const byName = new Map();
  for (const item of items) {
    let group = byName.get(item.group);
    if (!group) {
      group = { name: item.group, configured: false, items: [] };
      byName.set(item.group, group);
      groups.push(group);
    }
    group.items.push(item);
    if (item.configured) group.configured = true;
  }
  for (const p of addable) {
    let group = groups.find((g) => g.name !== RECENT && g.items.some((item) => item.provider.id === p.id));
    if (!group) {
      group = { name: p.name, configured: true, items: [] };
      const at = groups.findIndex((g) => g.name !== RECENT && g.name.localeCompare(p.name) > 0);
      groups.splice(at < 0 ? groups.length : at, 0, group);
    }
    group.addTo = p.id;
  }
  return groups;
}

export const isCurrent = (item, current) => Boolean(current) && item.model.id === current.model && item.provider.id === current.provider;

// The effort row belongs to the model in use, so it shows only while that
// model is among the listed rows: a provider opened without it shows none
// (owner, 2026-10-22).
export const effortShown = (rows, current) => rows.some((item) => isCurrent(item, current));

// The group shown first: the one with the current model (its own provider
// rather than Recently used), else the first set-up one, else the first.
export function startGroup(groups, current) {
  const own = groups.find((g) => g.name !== RECENT && g.items.some((item) => isCurrent(item, current)));
  return (own ?? groups.find((g) => g.configured) ?? groups[0])?.name ?? "";
}

// While searching: every model whose provider or name holds the words,
// spaces and case ignored, without Recently used's copies.
export function searchRows(items, query) {
  const words = String(query ?? "").replace(/\s+/g, "").toLowerCase();
  if (!words) return [];
  return items.filter((item) => !item.recent && `${item.group}${item.model.name || item.model.id}`.replace(/\s+/g, "").toLowerCase().includes(words));
}

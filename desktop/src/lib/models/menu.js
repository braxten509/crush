// The model menu over the model button (owner, 2026-10-19: picker "B"): the
// agents and providers in a column on the left, the chosen one's models on
// the right, effort under them. Built from lib/models/groups.js's rows.
export const RECENT = "Recently used";

// [{name, configured, items}] in the rows' order.
export function menuGroups(items) {
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
  return groups;
}

export const isCurrent = (item, current) => Boolean(current) && item.model.id === current.model && item.provider.id === current.provider;

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

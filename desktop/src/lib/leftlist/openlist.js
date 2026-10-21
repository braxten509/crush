// The left list's rows: the open chats the desktop shares with the terminal's
// session list (internal/openchats; owner, 2026-10-20: "the two should be in
// sync"), in the list's own order, one flat list with each chat's folder as
// its second line (owner's pick A). Pure, so the rules are unit-tested.
//
// A chat of the window's folder is drawn from the sessions store, with what
// it is doing; its open branches hang under it, or under the nearest open
// chat above a closed one (listmodel.buildRows). A chat
// of another folder carries its folder ({dir}) so a pick switches the window
// there first; its title and time come from the all-folder catalog when it
// has them, else from the list.

import { buildRows, rowTime } from "../listmodel.js";

/**
 * open: [{id, dir, title}] in the list's order. Options: byId (the sessions
 * store's records), parentOf, here(dir) → the window's folder, selectedId,
 * busy, now (ms), timeOf(session), folderOf(dir) → the second line, catalog
 * ({id: saved-sessions row}).
 */
export function listedRows(open, options = {}) {
  const {
    byId = {},
    parentOf = () => null,
    here = () => false,
    selectedId = "",
    busy = () => false,
    now = Date.now(),
    timeOf = (s) => s?.updated_at ?? 0,
    folderOf = (dir) => dir,
    catalog = {},
  } = options;
  const chats = (Array.isArray(open) ? open : []).filter((c) => c?.id);
  const listed = new Set(chats.map((c) => c.id));
  const dirOf = new Map(chats.map((c) => [c.id, c.dir]));
  // Every chat of the window's folder, unlisted ones closed: a closed chat
  // gives its place to its open branches.
  const built = buildRows(Object.values(byId), {
    parentOf,
    isClosed: (id) => !listed.has(id),
    selectedId,
    busy,
    now,
    timeOf,
    agentOf: (s) => folderOf(dirOf.get(s.id) ?? ""),
  });
  // Each top-level chat with the branches drawn under it.
  const families = new Map();
  let top = null;
  for (const row of built.rows) {
    if (row.depth === 0) {
      top = row.id;
      families.set(top, []);
    }
    families.get(top)?.push(row);
  }
  const rows = [];
  for (const c of chats) {
    if (families.has(c.id)) {
      rows.push(...families.get(c.id));
      continue;
    }
    if (here(c.dir) && byId[c.id]) continue; // a branch, under its chat
    const saved = catalog[c.id];
    const at = saved?.UpdatedAt ?? 0;
    rows.push({
      kind: "chat",
      id: c.id,
      title: saved?.Title || c.title || "New Session",
      agent: folderOf(c.dir),
      time: rowTime(at, now),
      at,
      depth: 0,
      selected: false,
      closed: false,
      state: "",
      stateKind: "",
      branches: 0,
      expanded: false,
      folder: here(c.dir) ? null : { dir: c.dir },
    });
  }
  return rows;
}

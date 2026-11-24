// The left list's rows: the open chats the desktop shares with the terminal's
// session list (internal/openchats; owner, 2026-10-20: "the two should be in
// sync"), in the list's own order, each with its folder ({dir}), then grouped
// under their folders (folderGroups). Pure, so the rules are unit-tested.
//
// A chat of the window's folder is drawn from the sessions store, with what
// it is doing; its open branches hang under it, or under the nearest open
// chat above a closed one (listmodel.buildRows). A chat of another folder
// carries its folder ({dir}) so a pick switches the window there first; its
// title, time and branch parent come from the all-folder catalog when it has
// them, else from the list, and it nests the same way.

import { buildRows, rowTime } from "../listmodel.js";

/**
 * open: [{id, dir, title}] in the list's order. Options: byId (the sessions
 * store's records), parentOf, here(dir) → the window's folder, selectedId,
 * busy, now (ms), timeOf(session), folderOf(dir) → the second line, catalog
 * ({id: saved-sessions row}), states ({id: what another folder's chat is
 * doing, from its engine}).
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
    states = {},
  } = options;
  const chats = (Array.isArray(open) ? open : []).filter((c) => c?.id);
  const listed = new Set(chats.map((c) => c.id));
  const dirOf = new Map(chats.map((c) => [c.id, c.dir]));
  // A chat of another folder has no record in the store: it is drawn from the
  // all-folder catalog and what its engine says it is doing, and its branch
  // parent comes from the catalog (BranchOf), so it nests as it would in its
  // own folder (owner, 2026-11-19: branches lost their place on a switch).
  const others = new Set();
  const records = Object.values(byId);
  for (const c of chats) {
    if (byId[c.id]) continue;
    others.add(c.id);
    const saved = catalog[c.id];
    const state = states[c.id] ?? {};
    const lock = state.lock_state || saved?.LockState || "";
    records.push({
      ...state,
      id: c.id,
      title: state.title || saved?.Title || c.title || "New Session",
      created_at: saved?.CreatedAt ?? 0,
      updated_at: saved?.UpdatedAt ?? 0,
      lock_state: lock,
    });
  }
  // A closed chat between an open branch and its open ancestor is added
  // closed, so the branch moves up under that ancestor, not to the top.
  for (const id of [...others]) {
    const seen = new Set([id]);
    for (let up = catalog[id]?.BranchOf; up && !seen.has(up) && !byId[up] && catalog[up]; up = catalog[up].BranchOf) {
      seen.add(up);
      if (others.has(up)) break;
      others.add(up);
      records.push({ id: up, title: catalog[up].Title || "New Session", created_at: catalog[up].CreatedAt ?? 0, updated_at: catalog[up].UpdatedAt ?? 0 });
    }
  }
  // Every chat, unlisted ones closed: a closed chat gives its place to its
  // open branches. Another folder has no selected chat to unfold a family,
  // so its branches always show (a pick there switches folders first).
  const built = buildRows(records, {
    parentOf: (id) => (others.has(id) ? catalog[id]?.BranchOf || null : parentOf(id)),
    isClosed: (id) => !listed.has(id),
    expanded: [...others],
    selectedId,
    busy: (id) => (others.has(id) ? Boolean(states[id]?.is_busy) : busy(id)),
    now,
    timeOf: (s) => (others.has(s.id) ? (s.updated_at ?? 0) : timeOf(s)),
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
  // A chat that is no family's head is drawn under its parent, or folded away.
  const rows = [];
  for (const c of chats) {
    for (const row of families.get(c.id) ?? []) {
      const dir = dirOf.get(row.id) ?? c.dir;
      rows.push({ ...row, dir, folder: here(dir) ? null : { dir } });
    }
  }
  return rows;
}

// New chat's temporary row while its empty chat saves, after rows: where and
// as the saved chat will show (owner, 2026-10-23: it flashed "New chat" at
// the top, then the chat landed at the bottom as "New Session").
export function draftRow(rows, dir, now) {
  const at = Math.floor(now / 1000);
  return [
    ...rows,
    {
      kind: "chat",
      id: "new-chat",
      title: "New Session",
      agent: "",
      time: rowTime(at, now),
      at,
      depth: 0,
      selected: true,
      closed: false,
      state: "",
      stateKind: "",
      branches: 0,
      expanded: false,
      folder: null,
      dir,
      draft: true,
    },
  ];
}

// Sub-agent chat rows ({parent}, helperchats.svelte.js) among the list's
// rows: each after its parent's row and the helper rows already there, one
// step in from it, so it is drawn as one of its parent's branches (owner,
// 2026-11-12); or at the end, not set in, when its parent is not listed.
export function placeHelperRows(rows, helpers) {
  const out = [...rows];
  for (const row of helpers) {
    const parent = out.findIndex((r) => r.id === row.parent);
    let at = parent < 0 ? out.length - 1 : parent;
    while (at + 1 < out.length && out[at + 1].helper) at += 1;
    out.splice(at + 1, 0, parent < 0 ? row : { ...row, depth: (out[parent].depth ?? 0) + 1 });
  }
  return out;
}

// The rows under their folders (owner, 2026-10-21): every kept folder in its
// kept order, then any other folder a row has, in the rows' order. A kept
// folder with no open chat still shows, with no rows. key(dir) makes one
// folder's paths equal.
export function folderGroups(rows, kept = [], key = (dir) => dir) {
  const order = [];
  for (const dir of [...kept, ...rows.map((r) => r.dir ?? "")]) {
    if (!dir) continue;
    const k = key(dir);
    if (k && !order.includes(k)) order.push(k);
  }
  return order.map((dir) => ({ dir, rows: rows.filter((r) => key(r.dir ?? "") === dir) }));
}

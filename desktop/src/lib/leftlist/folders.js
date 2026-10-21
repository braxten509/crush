// The other folders' chats in the left list (owner, 2026-10-20: the list shows
// every chat not closed, from any folder, under folder headings; the window's
// folder is the shown chat's). Pure, so the rules are unit-tested: the rows
// come from the all-folder catalog (GET saved-sessions, session.Session with
// its Directory and DataDirectory); this folder's chats are the sessions
// store's and are left out here, as are branches (they hang under their chats
// once their folder is shown) and chats closed in their own folder's set.

import { rowTime } from "../listmodel.js";

export function folderName(path) {
  const parts = String(path ?? "").split(/[\\/]/u).filter(Boolean);
  return parts[parts.length - 1] ?? "";
}

/**
 * catalog: saved-sessions rows. Options: here(dir) → true for this window's
 * folder; isClosedIn(dataDir, id); isBranch(dataDir, id); agentOf(provider)
 * → the agent's name; now (ms). Answers the groups, folders
 * with the newest chat first, each {key, label, path, rows} with its chats
 * newest first as ChatRow rows carrying their folder ({dir, dataDir}).
 */
export function folderGroups(catalog, { here = () => false, isClosedIn = () => false, isBranch = () => false, agentOf = () => "", now = Date.now() } = {}) {
  const byFolder = new Map();
  const seen = new Set();
  for (const s of Array.isArray(catalog) ? catalog : []) {
    if (!s?.ID || !s.Directory || s.ParentSessionID || seen.has(s.ID)) continue;
    seen.add(s.ID);
    if (here(s.Directory) || isClosedIn(s.DataDirectory ?? "", s.ID) || isBranch(s.DataDirectory ?? "", s.ID)) continue;
    if (!byFolder.has(s.Directory)) byFolder.set(s.Directory, []);
    byFolder.get(s.Directory).push(s);
  }
  const groups = [];
  for (const [dir, chats] of byFolder) {
    chats.sort((a, b) => (b.UpdatedAt ?? 0) - (a.UpdatedAt ?? 0));
    groups.push({
      key: `folder:${dir}`,
      label: folderName(dir),
      path: dir,
      newest: chats[0].UpdatedAt ?? 0,
      rows: chats.map((s) => ({
        kind: "chat",
        id: s.ID,
        title: s.Title || "New Session",
        agent: agentOf(s.ContextBudget?.Provider ?? ""),
        time: rowTime(s.UpdatedAt ?? 0, now),
        at: s.UpdatedAt ?? 0,
        depth: 0,
        selected: false,
        closed: false,
        state: "",
        stateKind: "",
        branches: 0,
        expanded: false,
        folder: { dir, dataDir: s.DataDirectory ?? "" },
      })),
    });
  }
  return groups.sort((a, b) => b.newest - a.newest);
}

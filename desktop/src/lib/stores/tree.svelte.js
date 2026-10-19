// Each chat's session tree, for P4.4's dialogs (desktop-design 3.3, 3.6;
// engine 5.6). The revision is the one this window last read or got back
// from its own action; a tree_changed with another revision only marks the
// tree stale, so a change another app made in between still meets 409
// revision_conflict (engine items 15, 17; 1.5.3). Links need no tree read:
// the loaded messages are the current path (3.3).
import { api } from "../engine/api.js";
import { onEvent } from "../engine/events.js";

// One per chat read or acted on; a class instance, so no store proxy wraps
// the entries.
export class SessionTree {
  revision = $state(null); // null until a read or an action result
  entries = $state.raw([]); // proto.TreeEntry[] as read
  stale = $state(false);
}

export const tree = $state({ bySession: {} }); // sid → { revision, entries, stale }

function treeFor(sid) {
  let t = tree.bySession[sid];
  if (!t) {
    t = new SessionTree();
    tree.bySession[sid] = t;
  }
  return t;
}

const reads = new Map(); // sid → the read on its way

// GET …/tree: revision and entries; stale false. Two calls while one read is
// on its way share it. Rejects with the engine's error.
export function readTree(sid) {
  if (!sid) return Promise.resolve(null);
  if (reads.has(sid)) return reads.get(sid);
  const read = (async () => {
    try {
      const result = await api.tree(sid);
      const t = treeFor(sid);
      t.revision = Number.isFinite(result?.revision) ? result.revision : null;
      t.entries = Array.isArray(result?.entries) ? result.entries : [];
      t.stale = false;
      return t;
    } finally {
      reads.delete(sid);
    }
  })();
  reads.set(sid, read);
  return read;
}

// After this window's own label, switch, navigate or copy: the result's
// revision (TreeResult.revision; for a copy TreeCopyResult.revision, the
// source chat's), so the next action needs no read (engine items 15, 17).
// The stale mark is left alone: a tree_changed of another app's change may
// have come before this result.
export function keepRevision(sid, revision) {
  if (!sid || !Number.isFinite(revision)) return;
  treeFor(sid).revision = revision;
}

// A tree_changed whose revision differs from the kept one (1.5.3). The kept
// revision stays: only a read or this window's own result sets it.
export function markStale(sid, revision) {
  const t = tree.bySession[sid];
  if (!t || t.revision === revision) return;
  t.stale = true;
}

// What the next action sends as expected_revision (null before any read).
export function expectedRevision(sid) {
  return tree.bySession[sid]?.revision ?? null;
}

onEvent("tree_changed", (_kind, change) => {
  if (change?.session_id) markStale(change.session_id, change.revision);
});

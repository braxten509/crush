// Each chat's session tree, for P4.4's dialogs (desktop-design 3.3, 3.6).
// Scaffold S3: the final exports with empty data; piece P4.1 fills them.

export const tree = $state({ bySession: {} }); // sid → { revision, entries, stale }

// GET …/tree: revision and entries; stale false
export async function readTree(sid) {}

// After this window's own label, switch, navigate or copy: the result's
// revision (TreeResult.revision; for a copy TreeCopyResult.revision, the
// source chat's), so the next action needs no read (engine items 15, 17).
export function keepRevision(sid, revision) {}

// A tree_changed whose revision differs from the kept one (1.5.3).
export function markStale(sid, revision) {}

// What the next action sends as expected_revision.
export function expectedRevision(sid) {}

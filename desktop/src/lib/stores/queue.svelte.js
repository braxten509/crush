// Queued prompts per chat (desktop-design 1.5.3). Scaffold S3: the pinned
// exports with empty bodies; piece P3.7 writes them.

export const queue = $state({ bySession: {} }); // sid → { count, items: [{ text, attachments, label }], loaded }

// GET …/prompts/queued and …/prompts/list?details=true
export async function refresh(sid) {}

// TYPE-050 (…/prompts/clear)
export async function clear(sid) {}

// TYPE-019 (…/prompts/recall) → { text, attachments }
export async function recall(sid) {}

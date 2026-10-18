// The chat list and the shown chat (desktop-design 1.5.3). Scaffold S3: the
// pinned exports with empty bodies; piece P3.3 writes them.

export const sessions = $state({
  byId: {},
  order: [],
  currentId: "",
  loaded: false,
  live: {}, // sid → true while a chat this window has not loaded is replying (1.5.3)
});

// fromEvent keeps is_busy, attached_clients, has_unfinished_work, pending_prompt.
export function upsertSession(s, { fromEvent = false } = {}) {}

export function removeSession(id) {}

// current-session, load messages, restore model (LIST-035)
export async function openChat(id) {}

// LIST-033: shows the empty chat; the record is made on first send (TYPE-041)
export async function newChat() {}

// The shown chat's id; on the landing page it creates "New Session" first (TYPE-041).
export async function ensureSession() {}

// Derived busy (1.5.3).
export function busy(id) {
  return false;
}

// Closed chats: since 2026-10-20 a chat is closed when it is off the open
// chats shared with the terminal (openchats.svelte.js; owner: "the two
// should be in sync"). The branch links, the title bar and the search keep
// these names: isClosed, reopen (lists the chat again) and closeChat (the
// list's ×: the shown chat makes way for the next listed chat of its folder,
// else the landing state).
import { closeListed, listChat, openChats } from "./openchats.svelte.js";
import { sessions } from "./sessions.svelte.js";

export function isClosed(sessionId) {
  return Boolean(sessionId) && openChats.loaded && !openChats.list.some((c) => c.id === sessionId);
}

// This folder's chats that are closed (the branch probe's list).
export const closed = {
  get ids() {
    return new Set(sessions.order.filter((id) => isClosed(id)));
  },
};

export async function reopen(sessionId) {
  return listChat(sessionId);
}

export async function closeChat(sessionId) {
  return closeListed(sessionId);
}

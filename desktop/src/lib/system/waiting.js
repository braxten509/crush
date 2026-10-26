// The chats this window sees waiting for an answer (a question or a
// permission ask), for the app-wide taskbar badge (owner, 2026-10-25): every
// open chat of every folder the left list holds, by pendingOf(id), and the
// shown chat while its form or ask is up.
export function waitingChats(openIds, pendingOf, shown, shownBlocked) {
  const out = new Set();
  for (const id of openIds) {
    const kind = pendingOf(id);
    if (kind === "question" || kind === "permission") out.add(id);
  }
  if (shown && shownBlocked) out.add(shown);
  return [...out].sort();
}

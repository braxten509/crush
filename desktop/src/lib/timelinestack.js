// The chats TimelineStack keeps drawn: the shown chat, then every listed chat
// whose messages are loaded, each once. The order only depends on the list,
// so a slot never moves when another chat is picked (a moved node loses its
// scroll spot).
export function stackedChats(shownId, listedIds, loaded) {
  const out = [];
  const seen = new Set();
  for (const id of listedIds) {
    if (!id || seen.has(id) || (id !== shownId && !loaded(id))) continue;
    seen.add(id);
    out.push(id);
  }
  if (shownId && !seen.has(shownId)) out.push(shownId);
  return out;
}

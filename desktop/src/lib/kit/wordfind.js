// The chat search's literal match (owner, 2026-11-16): every word of the
// query must appear in the text as typed, letters side by side, case folded;
// scattered letters no longer count. Rows keep their own order.
//
// wordFind(query, strings) → [{index, matched: number[]}], the same shape as
// fuzzyFind, with matched the JS string indexes of every letter of every
// place a query word appears, for underlining. An empty query matches
// nothing; FilterList shows every row then.

export function wordFind(query, strings) {
  const words = String(query ?? "")
    .toLowerCase()
    .split(/\s+/)
    .filter(Boolean);
  if (words.length === 0) return [];
  const found = [];
  for (let index = 0; index < strings.length; index++) {
    const text = String(strings[index] ?? "").toLowerCase();
    const marks = new Set();
    let all = true;
    for (const word of words) {
      let at = text.indexOf(word);
      if (at < 0) {
        all = false;
        break;
      }
      while (at >= 0) {
        for (let i = at; i < at + word.length; i++) marks.add(i);
        at = text.indexOf(word, at + word.length);
      }
    }
    if (all) found.push({ index, matched: [...marks].sort((a, b) => a - b) });
  }
  return found;
}

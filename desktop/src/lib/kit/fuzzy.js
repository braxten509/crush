// The filter lists' fuzzy match (desktop-design-5-6 2.1, DLG-010): a port of
// github.com/sahilm/fuzzy v0.1.3 FindFrom (fuzzy.go:110-244, 246-293), so a
// query ranks and marks the rows as the terminal's lists do: the same bonuses
// (first character 10, after a separator 20, camel case 20, adjacent 5 growing
// as current*2+5), the same leading penalty (-5 per unmatched leading
// character, floor -15), one point off per unmatched character, separators
// "/-_ .\", case folded the Unicode simple way, and a stable sort by score.
//
// Like Go, the score counts positions and lengths in UTF-8 bytes (a leading
// "é" costs two characters' penalty, as in the terminal). The matched
// positions handed back are JS string indexes (UTF-16) of the matched
// characters, for underlining. Text after a NUL character is not matched
// (fuzzy.go:149-155).
//
// fuzzyFind(query, strings) → [{index, score, matched: number[]}], best first.
// An empty query matches nothing, as FindFrom; FilterList shows every row in
// its own order then (internal/ui/list/filterable.go:91-102).

const FIRST_CHAR_MATCH_BONUS = 10;
const MATCH_FOLLOWING_SEPARATOR_BONUS = 20;
const CAMEL_CASE_MATCH_BONUS = 20;
const ADJACENT_MATCH_BONUS = 5;
const UNMATCHED_LEADING_CHAR_PENALTY = -5;
const MAX_UNMATCHED_LEADING_CHAR_PENALTY = -15;

const SEPARATORS = new Set(["/", "-", "_", " ", ".", "\\"]);
const LOWER = /^\p{Ll}$/u;
const UPPER = /^\p{Lu}$/u;

export function fuzzyFind(query, strings) {
  const pattern = [...String(query ?? "")];
  if (pattern.length === 0) return [];
  const folds = pattern.map(folder);
  const matches = [];
  for (let index = 0; index < strings.length; index++) {
    const found = matchOne(folds, String(strings[index] ?? ""));
    if (found) matches.push({ index, score: found.score, matched: found.matched });
  }
  // sort.Stable with Less = higher score; Array.prototype.sort is stable.
  return matches.sort((a, b) => b.score - a.score);
}

// One string against the pattern (the body of FindFromIterNoSort's loop).
function matchOne(folds, text) {
  const nul = text.indexOf("\0");
  const clean = nul > -1 ? text.slice(0, nul) : text;
  const chars = characters(clean);
  const length = chars.length > 0 ? chars[chars.length - 1].byte + chars[chars.length - 1].size : 0;

  let total = 0;
  const matchedBytes = [];
  const matched = [];
  let patternIndex = 0;
  let bestScore = -1;
  let matchedIndex = -1;
  let matchedAt = -1;
  let currAdjacentMatchBonus = 0;
  let last = ""; // the rune before the candidate (Go's zero rune at the start)
  let lastIndex = 0;

  for (let k = 0; k < chars.length; k++) {
    const { ch, byte: j, at } = chars[k];
    if (patternIndex < folds.length && folds[patternIndex](ch)) {
      let score = 0;
      if (j === 0) score += FIRST_CHAR_MATCH_BONUS;
      if (LOWER.test(last) && UPPER.test(ch)) score += CAMEL_CASE_MATCH_BONUS;
      if (j !== 0 && SEPARATORS.has(last)) score += MATCH_FOLLOWING_SEPARATOR_BONUS;
      if (matchedBytes.length > 0) {
        const lastMatch = matchedBytes[matchedBytes.length - 1];
        const bonus = lastMatch === lastIndex ? currAdjacentMatchBonus * 2 + ADJACENT_MATCH_BONUS : 0;
        score += bonus;
        // Adjacent matches keep growing the bonus, even for a candidate that
        // loses to a better one later (fuzzy.go:186-193).
        currAdjacentMatchBonus += bonus;
      }
      if (score > bestScore) {
        bestScore = score;
        matchedIndex = j;
        matchedAt = at;
      }
    }
    // The best candidate is kept when the next pattern character comes up next
    // or the string ends, so the match is the best one, not the first
    // ("tk" in "The Black Knight" takes the second k; fuzzy.go:212-229).
    const next = chars[k + 1];
    const nextPattern = folds[patternIndex + 1];
    if (next === undefined || (nextPattern !== undefined && nextPattern(next.ch))) {
      if (matchedIndex > -1) {
        if (matchedBytes.length === 0) {
          bestScore += Math.max(matchedIndex * UNMATCHED_LEADING_CHAR_PENALTY, MAX_UNMATCHED_LEADING_CHAR_PENALTY);
        }
        total += bestScore;
        matchedBytes.push(matchedIndex);
        matched.push(matchedAt);
        bestScore = -1;
        patternIndex++;
      }
    }
    lastIndex = j;
    last = ch;
  }
  // One point off for each character the pattern did not match (in bytes).
  total += matchedBytes.length - length;
  return matchedBytes.length === folds.length ? { score: total, matched } : null;
}

// The string's characters (code points) with their UTF-8 byte offset and size
// and their JS string index.
function characters(text) {
  const out = [];
  let byte = 0;
  for (let at = 0; at < text.length; ) {
    const cp = text.codePointAt(at);
    const ch = String.fromCodePoint(cp);
    const size = cp < 0x80 ? 1 : cp < 0x800 ? 2 : cp < 0x10000 ? 3 : 4;
    out.push({ ch, byte, size, at });
    byte += size;
    at += ch.length;
  }
  return out;
}

// Go's equalFold for one pattern character: equal, or the same under Unicode
// simple case folding, which is what a case-insensitive Unicode RegExp
// compares (so the Kelvin sign matches "k" and "ſ" matches "s", while "ı" does
// not match "I", as in Go).
function folder(p) {
  const code = p.codePointAt(0);
  const same = new RegExp(`^\\u{${code.toString(16)}}$`, "iu");
  if (code < 0x80) {
    const lower = p.toLowerCase();
    return (ch) => (ch.length === 1 && ch.charCodeAt(0) < 0x80 ? ch.toLowerCase() === lower : same.test(ch));
  }
  return (ch) => ch === p || same.test(ch);
}

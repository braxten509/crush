// The terminal's fuzzy matcher, github.com/sahilm/fuzzy v0.1.3 (fuzzy.go:138-293),
// as the `@` popup filters with it (internal/ui/list/filterable.go:105). Scores
// count UTF-8 bytes, as Go's do; `chars` gives the matched characters'
// positions for drawing them at weight 600 (TYPE-027).

const FIRST_CHAR_BONUS = 10;
const AFTER_SEPARATOR_BONUS = 20;
const CAMEL_CASE_BONUS = 20;
const ADJACENT_BONUS = 5;
const LEADING_PENALTY = -5;
const MAX_LEADING_PENALTY = -15;
const SEPARATORS = new Set(["/", "-", "_", " ", ".", "\\"]);

const utf8Size = (ch) => {
  const code = ch.codePointAt(0);
  return code < 0x80 ? 1 : code < 0x800 ? 2 : code < 0x10000 ? 3 : 4;
};
const isLower = (ch) => ch !== "" && /\p{Ll}/u.test(ch);
const isUpper = (ch) => ch !== "" && /\p{Lu}/u.test(ch);

// strings.EqualFold for one character each ("" stands for Go's rune 0).
function equalFold(a, b) {
  if (a === b) return true;
  if (a === "" || b === "") return false;
  return a.toLowerCase() === b.toLowerCase() || a.toUpperCase() === b.toUpperCase();
}

function score(pattern, text) {
  const nul = text.indexOf("\0");
  const clean = nul > -1 ? text.slice(0, nul) : text;
  const chars = [...clean];
  const offsets = [];
  let length = 0;
  for (const ch of chars) {
    offsets.push(length);
    length += utf8Size(ch);
  }
  const matched = [];
  const positions = [];
  let total = 0;
  let patternIndex = 0;
  let best = -1;
  let matchedIndex = -1;
  let matchedChar = -1;
  let adjacent = 0;
  let last = "";
  let lastIndex = 0;
  for (let k = 0; k < chars.length; k += 1) {
    const j = offsets[k];
    const candidate = chars[k];
    if (equalFold(candidate, pattern[patternIndex] ?? "")) {
      let value = 0;
      if (j === 0) value += FIRST_CHAR_BONUS;
      if (isLower(last) && isUpper(candidate)) value += CAMEL_CASE_BONUS;
      if (j !== 0 && SEPARATORS.has(last)) value += AFTER_SEPARATOR_BONUS;
      if (matched.length > 0) {
        const bonus = matched[matched.length - 1] === lastIndex ? adjacent * 2 + ADJACENT_BONUS : 0;
        value += bonus;
        adjacent += bonus;
      }
      if (value > best) {
        best = value;
        matchedIndex = j;
        matchedChar = k;
      }
    }
    const nextPattern = patternIndex < pattern.length - 1 ? pattern[patternIndex + 1] : "";
    const nextChar = k + 1 < chars.length ? chars[k + 1] : "";
    // The best score is applied when the next pattern character comes next or
    // the text ends, so the last pattern character finds its best place.
    if ((equalFold(nextPattern, nextChar) || nextChar === "") && matchedIndex > -1) {
      if (matched.length === 0) best += Math.max(matchedIndex * LEADING_PENALTY, MAX_LEADING_PENALTY);
      total += best;
      matched.push(matchedIndex);
      positions.push(matchedChar);
      best = -1;
      patternIndex += 1;
    }
    lastIndex = j;
    last = candidate;
  }
  total += matched.length - length;
  return matched.length === pattern.length ? { score: total, chars: positions } : null;
}

// fuzzy.Find: the texts that hold every character of the pattern in order,
// best score first; equal scores keep their order (sort.Stable).
export function fuzzyFind(pattern, texts) {
  if (pattern.length === 0) return [];
  const runes = [...pattern];
  const found = [];
  texts.forEach((text, index) => {
    const match = score(runes, text);
    if (match) found.push({ text, index, score: match.score, chars: match.chars });
  });
  return found.sort((a, b) => b.score - a.score || a.index - b.index);
}

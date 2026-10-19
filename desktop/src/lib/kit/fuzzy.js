// The filter lists' fuzzy match, a port of github.com/sahilm/fuzzy v0.1.3
// FindFrom (desktop-design-5-6 2.1). Placeholder from scaffold S5 with the
// pinned export; KIT owns it (7.3). Until then every string matches, in its
// own order, with no marks.

// → [{index, score, matched: number[]}]
export function fuzzyFind(query, strings) {
  return strings.map((_, index) => ({ index, score: 0, matched: [] }));
}

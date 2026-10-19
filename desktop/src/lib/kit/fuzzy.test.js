import { describe, expect, it } from "vitest";
import { fuzzyFind } from "./fuzzy.js";

// u:kit.fuzzy_parity (desktop-design-5-6 2.1, 7.3). Every expected value below
// is what github.com/sahilm/fuzzy v0.1.3 Find answers for the same pattern and
// string (run once with Go 1.27 against the module the terminal pins,
// go.mod:60): the score, and the matched characters as JS string indexes
// (Go's byte offsets turned into UTF-16 indexes). null: Go finds no match.
const TABLE = [
  ["a", "a", 10, [0]],
  ["abc", "abc", 30, [0, 1, 2]],
  ["abc", "aXbXc", 8, [0, 2, 4]],
  ["mod", "Switch Model", 16, [7, 8, 9]],
  ["sm", "Switch Model", 20, [0, 7]],
  ["sw", "Switch Model", 5, [0, 1]],
  ["tk", "The Black Knight", 16, [0, 10]],
  ["fb", "fooBar", 26, [0, 3]],
  ["fb", "foo_bar", 25, [0, 4]],
  ["fb", "foo-bar", 25, [0, 4]],
  ["fb", "foo.bar", 25, [0, 4]],
  ["fb", "foo/bar", 25, [0, 4]],
  ["fb", "foo\\bar", 25, [0, 4]],
  ["fb", "foo bar", 25, [0, 4]],
  ["b", "ab", -6, [1]],
  ["c", "abc", -12, [2]],
  ["d", "abcd", -18, [3]],
  ["e", "abcde", -19, [4]],
  ["f", "abcdef", -20, [5]],
  ["abc", "ab", null, null],
  ["xyz", "abc", null, null],
  ["ABC", "abc", 30, [0, 1, 2]],
  ["abc", "ABC", 30, [0, 1, 2]],
  ["é", "café", -19, [3]],
  ["cf", "café", 7, [0, 2]],
  ["éf", "éfoo", 12, [0, 1]],
  ["日本", "こんにちは日本", -29, [5, 6]],
  ["k", "K", 8, [0]],
  ["s", "ſ", 9, [0]],
  ["ı", "I", null, null],
  ["i", "İstanbul", null, null],
  ["ß", "ẞ", 8, [0]],
  ["ab", "a\u0000b", null, null],
  ["a", "a\u0000b", 10, [0]],
  ["aa", "aaaa", 13, [0, 1]],
  ["aaa", "aaaa", 29, [0, 1, 2]],
  ["fbr", "foo bar baz", 22, [0, 4, 6]],
  ["😀x", "a😀x", -4, [1, 3]],
  ["gl", "GoLang", 26, [0, 2]],
  ["ses", "Summarize Session", 16, [0, 8, 10]],
];

describe("fuzzyFind, a port of sahilm/fuzzy v0.1.3 FindFrom", () => {
  it("fuzzy_parity", () => {
    expect(TABLE).toHaveLength(40);
    for (const [pattern, text, score, matched] of TABLE) {
      const found = fuzzyFind(pattern, [text]);
      const got = found.length === 1 ? [found[0].score, found[0].matched] : [null, null];
      expect({ pattern, text, got }).toEqual({ pattern, text, got: [score, matched] });
    }
    // One list, ranked: Go's order of indexes and scores for "ses", a stable
    // sort that keeps the two 16s in their own order; non-matches left out.
    const list = ["Switch Model", "Session Tree", "Summarize Session", "New Session", "Toggle Sidebar", "Settings", "sessions", "Open Session"];
    const ranked = fuzzyFind("ses", list);
    expect(ranked.map((m) => m.index)).toEqual([6, 1, 3, 2, 7, 5]);
    expect(ranked.map((m) => m.score)).toEqual([25, 21, 17, 16, 16, 10]);
    // An empty query matches nothing, as FindFrom.
    expect(fuzzyFind("", list)).toEqual([]);
  });
});

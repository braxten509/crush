import { describe, expect, it } from "vitest";
import {
  ALGORITHMS,
  cleanPath,
  computeDiff,
  dirTrim,
  fileRows,
  fixLcs,
  goSortSlice,
  lineCounts,
  modifiedFiles,
  relPath,
  splitLines,
  validLcs,
} from "./modified.js";

// SIDE-008. The diff is a line-by-line port of go-udiff v0.4.1 (lcs/old.go);
// its tests below are that module's own (lcs/old_test.go, common_test.go)
// and the unified diffs of difftest/difftest.go, which GNU diff -u checks.

const chars = (s) => [...s];

function lcsText(seq, lcs) {
  return lcs.map((d) => seq.slice(d.x, d.x + d.len).join("")).join("");
}

function applyDiffs(before, diffs, after) {
  let out = [];
  let sofar = 0;
  for (const d of diffs) {
    if (sofar < d.start) out = out.concat(before.slice(sofar, d.start));
    out = out.concat(after.slice(d.replStart, d.replEnd));
    sofar = d.end;
  }
  return out.concat(before.slice(sofar));
}

// lcs/common_test.go Btests: [a, b, the acceptable longest common subsequences].
const BTESTS = [
  ["aaabab", "abaab", ["abab", "aaab"]], ["aabbba", "baaba", ["aaba"]], ["cabbx", "cbabx", ["cabx", "cbbx"]], ["c", "cb", ["c"]],
  ["aaba", "bbb", ["b"]], ["bbaabb", "b", ["b"]], ["baaabb", "bbaba", ["bbb", "baa", "bab"]], ["baaabb", "abbab", ["abb", "bab", "aab"]],
  ["baaba", "aaabba", ["aaba"]], ["ca", "cba", ["ca"]], ["ccbcbc", "abba", ["bb"]], ["ccbcbc", "aabba", ["bb"]], ["ccb", "cba", ["cb"]],
  ["caef", "axe", ["ae"]], ["bbaabb", "baabb", ["baabb"]], ["abcabba", "cbabac", ["caba", "baba", "cbba"]], ["3456aaa", "aaa", ["aaa"]],
  ["aaa", "aaa123", ["aaa"]], ["aabaa", "aacaa", ["aaaa"]], ["1a", "a", ["a"]], ["abab", "bb", ["bb"]], ["123", "ab", [""]], ["a", "b", [""]],
  ["abc", "123", [""]], ["aa", "aa", ["aa"]], ["abcde", "12345", [""]], ["aaa3456", "aaa", ["aaa"]], ["abcde", "12345a", ["a"]], ["ab", "123", [""]],
  ["1a2", "a", ["a"]], ["babaab", "cccaba", ["aba"]], ["aabbab", "cbcabc", ["bab"]], ["abaabb", "bcacab", ["baab"]], ["abaabb", "abaaaa", ["abaa"]],
  ["bababb", "baaabb", ["baabb"]], ["abbbaa", "cabacc", ["aba"]], ["aabbaa", "aacaba", ["aaaa", "aaba"]],
];

describe("the lcs port (go-udiff lcs/old_test.go)", () => {
  for (const name of ["forward", "backward", "twosided"]) {
    it(`TestAlgosOld: ${name}`, () => {
      for (const [a, b, want] of BTESTS) {
        const limit = a.length + b.length;
        for (const [x, y] of [[a, b], [b, a]]) {
          const { diffs, lcs } = computeDiff(chars(x), chars(y), ALGORITHMS[name], limit);
          expect(validLcs(lcs), `${x} ${y}`).toBe(true);
          expect(want, `${x} ${y}`).toContain(lcsText(chars(x), lcs));
          expect(applyDiffs(chars(x), diffs, chars(y)).join(""), `${x} ${y}`).toBe(y);
        }
      }
    });
  }

  it("TestIntOld: the two-sided search with long common runs around", () => {
    const [lfill, rfill] = ["AAAAAAAAAAAA", "BBBBBBBBBBBB"];
    for (const [a, b, want] of BTESTS) {
      if (a.length < 2 || b.length < 2) continue;
      const limit = a.length + b.length;
      for (const [left, right] of [[a + lfill, b + rfill], [lfill + a, rfill + b]]) {
        for (const [x, y] of [[left, right], [right, left]]) {
          const { diffs, lcs } = computeDiff(chars(x), chars(y), ALGORITHMS.twosided, limit);
          expect(validLcs(lcs)).toBe(true);
          expect(want).toContain(lcsText(chars(x), lcs));
          expect(applyDiffs(chars(x), diffs, chars(y)).join("")).toBe(y);
        }
      }
    }
  });

  it("TestSpecialOld and the regressions: every search limit gives a valid lcs and a diff that applies", () => {
    const special = computeDiff(chars("golang.org/x/tools/intern"), chars('github.com/google/safehtml/template"\n\t"golang.org/x/tools/intern'), ALGORITHMS.twosided, 4);
    expect(validLcs(special.lcs)).toBe(true);
    const regressions = [
      [
        '// Copyright 2019 The Go Authors. All rights reserved.\n// Use of this source code is governed by a BSD-style\n// license that can be found in the LICENSE file.\n\npackage diff_test\n\nimport (\n\t"fmt"\n\t"math/rand"\n\t"strings"\n\t"testing"\n\n\t"golang.org/x/tools/gopls/internal/lsp/diff"\n\t"github.com/aymanbagabas/go-udiff/difftest"\n\t"golang.org/x/tools/gopls/internal/span"\n)\n',
        '// Copyright 2019 The Go Authors. All rights reserved.\n// Use of this source code is governed by a BSD-style\n// license that can be found in the LICENSE file.\n\npackage diff_test\n\nimport (\n\t"fmt"\n\t"math/rand"\n\t"strings"\n\t"testing"\n\n\t"github.com/google/safehtml/template"\n\t"golang.org/x/tools/gopls/internal/lsp/diff"\n\t"github.com/aymanbagabas/go-udiff/difftest"\n\t"golang.org/x/tools/gopls/internal/span"\n)\n',
        "b",
      ],
      ['n"\n)\n', 'n"\n\t"golang.org/x//nnal/stack"\n)\n', "b+"],
      ["golang.org/x/hello v1.0.0\nrequire golang.org/x/unused v1", "golang.org/x/hello v1", "a+"],
    ];
    for (const [a, b, range] of regressions) {
      const top = range === "b" ? b.length - 1 : range === "b+" ? b.length : a.length;
      for (let limit = 1; limit <= top; limit++) {
        const { diffs, lcs } = computeDiff(chars(a), chars(b), ALGORITHMS.twosided, limit);
        expect(validLcs(lcs), `limit ${limit}`).toBe(true);
        expect(applyDiffs(chars(a), diffs, chars(b)).join(""), `limit ${limit}`).toBe(b);
      }
    }
  });

  it("TestLcsFix: overlapping parts are trimmed to a valid lcs of the same length", () => {
    const d = (x, y, len) => ({ x, y, len });
    const cases = [
      [[d(0, 0, 3), d(2, 2, 5), d(3, 4, 5), d(8, 9, 4)], [d(0, 0, 2), d(2, 2, 1), d(3, 4, 5), d(8, 9, 4)]],
      [[d(1, 1, 6), d(6, 12, 3)], [d(1, 1, 5), d(6, 12, 3)]],
      [[d(0, 0, 4), d(3, 5, 4)], [d(0, 0, 3), d(3, 5, 4)]],
      [[d(0, 20, 1), d(0, 0, 3), d(1, 20, 4)], [d(0, 0, 3), d(3, 22, 2)]],
      [[d(0, 0, 4), d(1, 1, 2)], [d(0, 0, 4)]],
      [[d(0, 0, 4)], [d(0, 0, 4)]],
      [[], []],
      [[d(0, 0, 4), d(1, 1, 6), d(3, 3, 2)], [d(0, 0, 1), d(1, 1, 6)]],
    ];
    const total = (l) => l.reduce((n, x) => n + x.len, 0);
    for (const [before, after] of cases) {
      const got = fixLcs(before.map((x) => ({ ...x })));
      expect(got.length).toBe(after.length);
      expect(total(got)).toBe(total(after));
      expect(validLcs(got)).toBe(true);
    }
  });
});

describe("goSortSlice (Go's sort.Slice)", () => {
  it("sorts like any sort, and keeps Go's own order for ties", () => {
    const rnd = lcg(7);
    for (let n = 0; n < 300; n += 7) {
      const items = Array.from({ length: n }, (_, i) => ({ key: rnd(10), i }));
      const sorted = goSortSlice(items.slice(), (p, q) => p.key < q.key);
      expect(sorted.map((x) => x.key)).toEqual(items.map((x) => x.key).sort((p, q) => p - q));
    }
    // Twelve or fewer: insertion sort, which keeps ties in order.
    const small = goSortSlice([3, 1, 2, 1].map((key, i) => ({ key, i })), (p, q) => p.key < q.key);
    expect(small.map((x) => x.i)).toEqual([1, 3, 2, 0]);
  });
});

function lcg(seed) {
  let state = seed >>> 0;
  return (n) => {
    state = (Math.imul(state, 1664525) + 1013904223) >>> 0;
    return (state >>> 8) % n;
  };
}

// The length of a longest common subsequence, by plain dynamic programming.
function lcsLength(a, b) {
  const row = new Array(b.length + 1).fill(0);
  for (let i = 1; i <= a.length; i++) {
    let diag = 0;
    for (let j = 1; j <= b.length; j++) {
      const up = row[j];
      row[j] = a[i - 1] === b[j - 1] ? diag + 1 : Math.max(row[j], row[j - 1]);
      diag = up;
    }
  }
  return row[b.length];
}

describe("lineCounts (diff.GenerateDiff)", () => {
  // difftest/difftest.go: the unified diffs GNU diff -u gives; NoDiff cases left out.
  const cases = [
    ["", "", 0, 0], ["gargantuan\n", "gargantuan\n", 0, 0], ["fruit\n", "cheese\n", 1, 1], ["gord\n", "gourd\n", 1, 1], ["groat\n", "goat\n", 1, 1],
    ["loud\n", "lord\n", 1, 1], ["blanket\n", "bunker\n", 1, 1], ["1: one\n3: three\n", "1: one\n2: two\n3: three\n", 1, 0], ["A", "B", 1, 1],
    ["meow", "", 0, 1], ["", "AB\nC", 2, 0], ["A", "AB", 1, 1], ["A", "A\n", 1, 1], ["A\nB\n", "A\nC\n\n", 2, 1], ["\nA\n", "A\n", 0, 1],
    ["aaa\nccc\n", "aaa\nbbb\nccc\n", 1, 0],
    ["package a\n\ntype S struct {\ns fmt.Stringer\n}\n", "package a\n\ntype S struct {\n\ts fmt.Stringer\n}\n", 1, 1],
  ];
  it("the unified diffs of go-udiff's test data", () => {
    for (const [before, after, additions, deletions] of cases) expect(lineCounts(before, after), JSON.stringify([before, after])).toEqual({ additions, deletions });
  });

  it("a changed line starting ++ (added) or -- (removed) is not counted, as in the terminal", () => {
    expect(lineCounts("a\n", "a\n++x\n+y\n")).toEqual({ additions: 1, deletions: 0 });
    expect(lineCounts("a\n--x\n-y\n", "a\n")).toEqual({ additions: 0, deletions: 1 });
    expect(lineCounts("--x\n", "++x\n")).toEqual({ additions: 0, deletions: 0 });
  });

  // Each random edit moves the two texts at most two lines apart, and the
  // two-sided search finds the smallest diff up to 99 lines apart.
  it("under 50 edits the counts are the smallest diff's; past that the diff still applies", () => {
    const rnd = lcg(20261018);
    const vocab = ["a\n", "b\n", "c\n", "d\n", "e\n", "f\n", "\n", "last"];
    for (let c = 0; c < 400; c++) {
      const big = rnd(4) === 0;
      const before = Array.from({ length: rnd(big ? 300 : 40) }, () => vocab[rnd(vocab.length - 1)]);
      const after = before.slice();
      const edits = rnd(big ? 150 : 20);
      for (let e = 0; e < edits; e++) {
        const op = rnd(3);
        if (op === 0) after.splice(rnd(after.length + 1), 0, vocab[rnd(vocab.length - 1)]);
        else if (after.length > 0) after.splice(rnd(after.length), 1, ...(op === 2 ? [vocab[rnd(vocab.length - 1)]] : []));
      }
      if (rnd(4) === 0) after.push("last");
      const [a, b] = [before.join(""), after.join("")];
      const [left, right] = [splitLines(a), splitLines(b)];
      const { diffs } = computeDiff(left, right);
      expect(applyDiffs(left, diffs, right).join(""), `case ${c}`).toBe(b);
      const { additions, deletions } = lineCounts(a, b);
      const common = lcsLength(left, right);
      if (edits < 50) expect([additions, deletions], `case ${c}`).toEqual([right.length - common, left.length - common]);
      else expect(additions + deletions, `case ${c}`).toBeGreaterThanOrEqual(right.length + left.length - 2 * common);
    }
  });

  it("a large file with a small change is counted exactly and quickly", () => {
    const lines = Array.from({ length: 20_000 }, (_, i) => `line ${i}\n`);
    const after = lines.slice();
    after.splice(10_000, 3, "changed\n");
    const started = performance.now();
    expect(lineCounts(lines.join(""), after.join(""))).toEqual({ additions: 1, deletions: 3 });
    expect(performance.now() - started).toBeLessThan(500);
  });
});

describe("the file list (session.go:151-299)", () => {
  const file = (id, path, version, content, updated) => ({ id, session_id: "s", path, version, content, created_at: version, updated_at: updated });

  it("one entry per path, first version against latest, newest change first, unchanged ones kept for the caller", () => {
    const history = [
      file("1", "/p/a.go", 0, "x\n", 10),
      file("2", "/p/b.go", 0, "", 11),
      file("3", "/p/a.go", 1, "x\ny\n", 12),
      file("4", "/p/b.go", 1, "one\ntwo\n", 20),
      file("5", "/p/a.go", 2, "y\nz\n", 15),
      file("6", "/p/c.go", 0, "same\n", 30),
    ];
    expect(modifiedFiles(history)).toEqual([
      { path: "/p/c.go", additions: 0, deletions: 0, updatedAt: 30 },
      { path: "/p/b.go", additions: 2, deletions: 0, updatedAt: 20 },
      { path: "/p/a.go", additions: 2, deletions: 1, updatedAt: 15 },
    ]);
  });

  it("leaves anything with test in its path out of the list", () => {
    const history = [
      file("1", "/p/src/app.ts", 0, "a\n", 10),
      file("2", "/p/src/app.test.ts", 0, "a\n", 11),
      file("3", "/p/tests/app.ts", 0, "a\n", 12),
      file("4", "/p/src/LATEST.ts", 0, "a\n", 13),
    ];
    expect(modifiedFiles(history).map((f) => f.path)).toEqual(["/p/src/app.ts"]);
  });

  it("rows: changed files only, relative and trimmed paths, …and N more, nothing without changes", () => {
    const files = [
      { path: "/p/internal/ui/model/session.go", additions: 3, deletions: 0 },
      { path: "/p/a.go", additions: 0, deletions: 0 },
      { path: "/p/auth/session.go", additions: 0, deletions: 4 },
      { path: "/elsewhere/x/y.txt", additions: 1, deletions: 1 },
    ];
    expect(fileRows(files, { cwd: "/p" })).toEqual({
      rows: [
        { path: "~/.../m/session.go", fullPath: "/p/internal/ui/model/session.go", additions: 3, deletions: 0 },
        { path: "auth/session.go", fullPath: "/p/auth/session.go", additions: 0, deletions: 4 },
        { path: "~/.../x/y.txt", fullPath: "/elsewhere/x/y.txt", additions: 1, deletions: 1 },
      ],
      more: "",
    });
    const many = Array.from({ length: 25 }, (_, i) => ({ path: `/p/f${i}.go`, additions: 1, deletions: 0 }));
    const cut = fileRows(many, { cwd: "/p", max: 20 });
    expect([cut.rows.length, cut.more]).toEqual([20, "…and 5 more"]);
    expect(fileRows([{ path: "/p/a", additions: 0, deletions: 0 }], { cwd: "/p" })).toEqual({ rows: [], more: "" });
  });

  it("dirTrim, relPath and cleanPath as Go's", () => {
    expect(dirTrim("a.go", 2)).toBe("a.go");
    expect(dirTrim("auth/session.go", 2)).toBe("auth/session.go");
    expect(dirTrim("internal/ui/model.go", 2)).toBe("~/u/model.go");
    expect(dirTrim("a/b/c/d", 2)).toBe("~/.../c/d");
    expect(dirTrim("src/.config/x", 2)).toBe("~/x"); // "./x" cleaned
    expect(dirTrim("../../x/y.go", 2)).toBe("~/.../x/y.go");
    expect(dirTrim("docs/éclair/notes.md", 2)).toBe("~/é/notes.md");
    expect(relPath("/p", "/p/a/b.go")).toBe("a/b.go");
    expect(relPath("/p/q", "/p/a")).toBe("../a");
    expect(relPath("/p", "/p")).toBe(".");
    expect(relPath("/p", "rel/x")).toBe(null);
    expect(cleanPath("/a/./b/../c//d/")).toBe("/a/c/d");
    expect(cleanPath("../x/..")).toBe("..");
    expect(cleanPath("")).toBe(".");
  });
});

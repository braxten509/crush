// lib/diff.js against the terminal's own fixtures (CHAT-107, CHAT-108,
// CHAT-111, CHAT-112): go-udiff's goldens in internal/ui/diffview/testdata
// (TestUdiff) and every diff view golden of diffview_test.go, colors removed.
// The view goldens are drawn here from diffRows' rows with the terminal's
// column rules (render.go), so a hunk, a number, a pairing, the cap's "…" row
// or an offset that differs from Go fails.
import { readFileSync, readdirSync } from "node:fs";
import { describe, expect, test } from "vitest";
import {
  DELETE,
  EQUAL,
  INSERT,
  cells,
  charCells,
  diffRows,
  diffSequences,
  escapeControl,
  generateDiff,
  goSortSlice,
  hunkToSplit,
  lineEdits,
  numberDigits,
  prepareFile,
  tintFor,
  toUnified,
  unifiedDiff,
  unifiedText,
} from "./diff.js";

const TESTDATA = new URL("../../../internal/ui/diffview/testdata/", import.meta.url);
const read = (name) => readFileSync(new URL(name, TESTDATA), "utf8");
const strip = (text) => text.replace(/\x1b\[[0-9;:]*[A-Za-z]/g, "");
const golden = (name) => {
  const text = strip(read(name));
  expect(text.includes("\x1b"), `${name} has an escape the test does not strip`).toBe(false);
  return text;
};

// TestUdiff's before and after, read from udiff_test.go itself.
const UDIFF_TEST = readFileSync(new URL("../udiff_test.go", TESTDATA), "utf8");
const BEFORE = /before := `([^`]*)`/.exec(UDIFF_TEST)[1];
const AFTER = /after := `([^`]*)`/.exec(UDIFF_TEST)[1];

describe("go-udiff", () => {
  test("Unified matches TestUdiff/Unified.golden", () => {
    expect(unifiedDiff("main.go", "main.go", BEFORE, AFTER)).toBe(read("TestUdiff/Unified.golden"));
  });

  const KIND = { [DELETE]: 0, [INSERT]: 1, [EQUAL]: 2 };
  for (const [name, context] of [
    ["DefaultContextLines", 3],
    ["DefaultContextLinesPlusOne", 4],
    ["DefaultContextLinesPlusTwo", 5],
  ]) {
    test(`ToUnifiedDiff ${name}: text and hunks`, () => {
      const u = toUnified("main.go", "main.go", BEFORE, lineEdits(BEFORE, AFTER), context);
      expect(unifiedText(u)).toBe(read(`TestUdiff/ToUnifiedDiff/${name}/Content.golden`));
      const json = JSON.parse(read(`TestUdiff/ToUnifiedDiff/${name}/JSON.golden`));
      expect({
        From: u.from,
        To: u.to,
        Hunks: u.hunks.map((h) => ({ FromLine: h.fromLine, ToLine: h.toLine, Lines: h.lines.map((l) => ({ Kind: KIND[l.kind], Content: l.content })) })),
      }).toEqual(json);
    });
  }

  test("equal texts have no hunks and no text", () => {
    expect(lineEdits("a\nb\n", "a\nb\n")).toEqual([]);
    expect(unifiedDiff("x", "x", "a\n", "a\n")).toBe("");
  });

  test("an added file and a file without a final newline", () => {
    expect(unifiedDiff("f", "f", "", "one\ntwo\n")).toBe("--- f\n+++ f\n@@ -0,0 +1,2 @@\n+one\n+two\n");
    expect(unifiedDiff("f", "f", "a\nb", "a\nc")).toBe("--- f\n+++ f\n@@ -1,2 +1,2 @@\n a\n-b\n\\ No newline at end of file\n+c\n\\ No newline at end of file\n");
  });

  test("a hunk after a joined hunk keeps Go's new-side start (unified.go:135-137)", () => {
    const before = Array.from({ length: 20 }, (_, i) => `L${i}\n`).join("");
    const after = before.replace("L1\n", "").replace("L3\n", "").replace("L15\n", "X\n");
    const u = toUnified("f", "f", before, lineEdits(before, after), 3);
    expect(u.hunks.map((h) => [h.fromLine, h.toLine])).toEqual([
      [1, 1],
      [13, 10],
    ]);
  });

  test("past the search limit the partial answers are joined and repaired", () => {
    // More than 100 differences: the two-sided search stops at 50 steps each
    // way, and lcs.fix keeps the longest diagonals that fit.
    const a = [];
    const b = [];
    for (let i = 0; i < 160; i++) {
      a.push(`same ${i}\n`, `old ${i}\n`);
      b.push(`same ${i}\n`, `new ${i}\n`);
    }
    const diffs = diffSequences(a, b);
    // Whatever the search found, applying the edits must give b.
    const out = [];
    let at = 0;
    for (const d of diffs) {
      out.push(...a.slice(at, d.start), ...b.slice(d.replStart, d.replEnd));
      at = d.end;
    }
    out.push(...a.slice(at));
    expect(out).toEqual(b);
    expect(diffs.length).toBeGreaterThan(0);
  });

  test("Go's sort.Slice: insertion sort keeps ties up to 12 items, pdqsort beyond", () => {
    const small = Array.from({ length: 12 }, (_, i) => ({ k: i % 3, i }));
    goSortSlice(small, (p, q) => p.k < q.k);
    expect(small.map((x) => x.i)).toEqual([0, 3, 6, 9, 1, 4, 7, 10, 2, 5, 8, 11]);
    const big = Array.from({ length: 200 }, (_, i) => (i * 7919) % 200);
    goSortSlice(big, (p, q) => p < q);
    expect(big).toEqual([...big].sort((p, q) => p - q));
  });

  test("GenerateDiff names and counts", () => {
    const { diff, additions, removals } = generateDiff("a\nb\n", "a\nc\nd\n", "/src/x.go");
    expect(diff.startsWith("--- a/src/x.go\n+++ b/src/x.go\n")).toBe(true);
    expect([additions, removals]).toEqual([2, 1]);
  });
});

describe("side by side pairing (split.go)", () => {
  test("a deleted line pairs with the next insert; extra inserts stand alone", () => {
    const h = {
      fromLine: 1,
      toLine: 1,
      lines: [
        { kind: EQUAL, content: "a\n" },
        { kind: DELETE, content: "b\n" },
        { kind: DELETE, content: "c\n" },
        { kind: INSERT, content: "B\n" },
        { kind: INSERT, content: "C\n" },
        { kind: INSERT, content: "D\n" },
        { kind: EQUAL, content: "e\n" },
        { kind: DELETE, content: "f\n" },
        { kind: EQUAL, content: "g\n" },
        { kind: INSERT, content: "h\n" },
      ],
    };
    const pairs = hunkToSplit(h).lines.map((l) => [l.before?.content ?? null, l.after?.content ?? null]);
    expect(pairs).toEqual([
      ["a\n", "a\n"],
      ["b\n", "B\n"],
      ["c\n", "C\n"],
      [null, "D\n"],
      ["e\n", "e\n"],
      ["f\n", null],
      ["g\n", "g\n"],
      [null, "h\n"],
    ]);
  });
});

// -- The terminal's view, drawn from diffRows' rows (render.go, diffview.go) --

function truncate(s, length, tail) {
  if (cells(s) <= length) return s;
  length -= cells(tail);
  if (length < 0) return "";
  let out = "";
  let w = 0;
  for (const ch of s) {
    w += charCells(ch);
    if (w > length) return out + tail;
    out += ch;
  }
  return out + tail;
}
const pad = (s, n) => (n > 0 && cells(s) < n ? s + " ".repeat(n - cells(s)) : s);
const maxWidth = (s, n) => (n > 0 ? truncate(s, n, "") : s);
const padLeft = (v, n) => {
  const s = String(v);
  return cells(s) >= n ? s : " ".repeat(n - cells(s)) + s;
};

// dv.String() with colors removed: before/after files, as diffview_test.go sets them.
function terminalView({ path, before, after }, { layout = "unified", lineNumbers = true, width = 0, height = 0, xOffset = 0, yOffset = 0, infinite = false, contextLines = 3, tabWidth = 8 } = {}) {
  const file = prepareFile({ path, before, after }, { contextLines, tabWidth });
  const split = layout === "split";
  const [d1, d2] = numberDigits([file]);
  const hunkLine = (h) => `  @@ -${h.fromLine},${shown(h)[0]} +${h.toLine},${shown(h)[1]} @@ `;
  let codeWidth = 0;
  let extra = 0;
  if (width <= 0) {
    for (const h of file.hunks) {
      const header = cells(hunkLine(h));
      const contents = split ? hunkToSplit(h).lines.flatMap((l) => [l.before, l.after].filter(Boolean)) : h.lines;
      for (const l of contents) codeWidth = Math.max(codeWidth, cells(l.content.replace(/\n$/, "")) + 1, header);
    }
  } else {
    const numbers = d1 + d2 + 4;
    if (split) {
      const remaining = width - numbers - 4;
      codeWidth = Math.trunc(remaining / 2);
      extra = remaining % 2 !== 0 ? 1 : 0;
    } else {
      codeWidth = width - numbers - 2;
    }
  }
  const full = codeWidth + 2;
  const num = (v, d) => (lineNumbers ? ` ${padLeft(v, d)} ` : "");
  const content = (cell) => ({ text: truncate(cell.text, codeWidth, "…"), lead: cell.lead });
  const { rows } = diffRows([file], { layout, height, xOffset, yOffset, infinite, names: false });
  const hunks = file.hunks.slice();
  const out = [];
  for (const row of rows) {
    if (row.type === "hunk") {
      const text = truncate(`  ${row.text} `, full, "…");
      if (split) out.push(num("…", d1) + pad(text, full) + num("…", d2) + pad(" ", full + extra));
      else out.push(num("…", d1) + num("…", d2) + pad(text, full));
      hunks.shift();
    } else if (row.type === "line") {
      const c = content(row);
      if (row.kind === EQUAL) out.push(num(row.before, d1) + num(row.after, d2) + maxWidth(pad((c.lead ? " …" : "  ") + c.text, full), full));
      else if (row.kind === INSERT) out.push(num(" ", d1) + num(row.after, d2) + maxWidth((c.lead ? "+…" : "+ ") + pad(c.text, codeWidth), full));
      else out.push(num(row.before, d1) + num(" ", d2) + maxWidth((c.lead ? "-…" : "- ") + pad(c.text, codeWidth), full));
    } else if (row.type === "pair") {
      let line = "";
      const l = row.left;
      if (!l) line += num(" ", d1) + maxWidth(pad("  ", full), full);
      else {
        const c = content(l);
        if (l.kind === EQUAL) line += num(l.number, d1) + maxWidth(pad((c.lead ? " …" : "  ") + c.text, full), full);
        else line += num(l.number, d1) + maxWidth((c.lead ? "-…" : "- ") + pad(c.text, codeWidth), full);
      }
      const r = row.right;
      if (!r) line += num(" ", d2) + maxWidth(pad("  ", full + extra), full + extra);
      else {
        const c = content(r);
        if (r.kind === EQUAL) line += num(r.number, d2) + maxWidth(pad((c.lead ? " …" : "  ") + c.text, full + extra), full + extra);
        else line += num(r.number, d2) + maxWidth((c.lead ? "+…" : "+ ") + pad(c.text, codeWidth + extra), full + extra);
      }
      out.push(line);
    } else if (row.type === "more") {
      if (split) out.push(num("…", d1) + maxWidth(pad("  …", full), full) + num("…", d2) + maxWidth(pad("  …", full), full + extra));
      else out.push(num("…", d1) + num("…", d2) + maxWidth(pad("  …", full), full));
    }
  }
  let lines = out;
  if (width > 0) lines = lines.map((l) => truncate(l, width, ""));
  if (height > 0) lines = lines.slice(0, height);
  return lines.join("\n");
}

function shown(h) {
  let before = 0;
  let after = 0;
  for (const l of h.lines) {
    if (l.kind !== INSERT) before++;
    if (l.kind !== DELETE) after++;
  }
  return [before, after];
}

const file = (name, path) => ({ path, before: read(`${name}.before`), after: read(`${name}.after`) });
const DEFAULT = file("TestDefault", "main.go");
const MULTIPLE = file("TestMultipleHunks", "main.go");
const NARROW = file("TestNarrow", "text.txt");
const BEHAVIORS = {
  Default: [DEFAULT, {}],
  NoLineNumbers: [DEFAULT, { lineNumbers: false }],
  MultipleHunks: [MULTIPLE, {}],
  CustomContextLines: [MULTIPLE, { contextLines: 4 }],
  Narrow: [NARROW, {}],
  SmallWidth: [MULTIPLE, { width: 40 }],
  LargeWidth: [MULTIPLE, { width: 120 }],
  NoSyntaxHighlight: [MULTIPLE, {}],
};
const LAYOUTS = { Unified: "unified", Split: "split" };

describe("the terminal's diff view goldens (diffview_test.go)", () => {
  for (const [layoutName, layout] of Object.entries(LAYOUTS)) {
    for (const [behavior, [f, opts]] of Object.entries(BEHAVIORS)) {
      test(`TestDiffView/${layoutName}/${behavior}`, () => {
        const want = golden(`TestDiffView/${layoutName}/${behavior}/LightMode.golden`);
        expect(golden(`TestDiffView/${layoutName}/${behavior}/DarkMode.golden`)).toBe(want);
        expect(terminalView(f, { layout, ...opts })).toBe(want);
      });
    }
    test(`TestDiffViewTabs/${layoutName}`, () => {
      expect(terminalView(file("TestTabs", "main.go"), { layout })).toBe(golden(`TestDiffViewTabs/${layoutName}.golden`));
    });
    test(`TestDiffViewLineBreakIssue/${layoutName}`, () => {
      expect(terminalView(file("TestLineBreakIssue", "index.js"), { layout })).toBe(golden(`TestDiffViewLineBreakIssue/${layoutName}.golden`));
    });
    test(`TestDiffViewHeight/${layoutName}: the cap's "…" row`, () => {
      for (let height = 1; height <= 20; height++) {
        const want = golden(`TestDiffViewHeight/${layoutName}/HeightOf${String(height).padStart(3, "0")}.golden`);
        expect(terminalView(MULTIPLE, { layout, height }), `height ${height}`).toBe(want);
      }
    });
    test(`TestDiffViewXOffset/${layoutName}: the left "…" mark`, () => {
      for (let xOffset = 0; xOffset <= 20; xOffset++) {
        const want = golden(`TestDiffViewXOffset/${layoutName}/XOffsetOf${String(xOffset).padStart(2, "0")}.golden`);
        expect(terminalView(DEFAULT, { layout, width: 60, xOffset }), `x offset ${xOffset}`).toBe(want);
      }
    });
    for (const [kind, infinite] of [
      ["TestDiffViewYOffset", false],
      ["TestDiffViewYOffsetInfinite", true],
    ]) {
      test(`${kind}/${layoutName}`, () => {
        for (let yOffset = 0; yOffset <= 16; yOffset++) {
          const want = golden(`${kind}/${layoutName}/YOffsetOf${String(yOffset).padStart(2, "0")}.golden`);
          expect(terminalView(MULTIPLE, { layout, height: 5, yOffset, infinite }), `y offset ${yOffset}`).toBe(want);
        }
      });
    }
    test(`TestDiffViewWidth/${layoutName}`, () => {
      const names = readdirSync(new URL(`TestDiffViewWidth/${layoutName}/`, TESTDATA)).filter((n) => n.endsWith(".golden"));
      expect(names.length).toBe(layoutName === "Unified" ? 60 : 110);
      for (const name of names) {
        const width = Number(/WidthOf(\d+)/.exec(name)[1]);
        expect(terminalView(MULTIPLE, { layout, width }), `width ${width}`).toBe(golden(`TestDiffViewWidth/${layoutName}/${name}`));
      }
    });
  }
});

describe("the desktop view's own rules", () => {
  test("CRLF becomes LF and a tab four spaces (CHAT-107)", () => {
    const f = prepareFile({ path: "a.go", before: "x\r\n\ty\r\n", after: "x\r\n\tz\r\n" });
    expect(f.hunks[0].lines.map((l) => l.content)).toEqual(["x\n", "    y\n", "    z\n"]);
  });

  test("an empty diff has no rows", () => {
    const f = prepareFile({ path: "a.go", before: "same\n", after: "same\n" });
    expect(diffRows([f]).rows).toEqual([]);
  });

  test("NUL and DEL are drawn as pictures, never raw (CHAT-110)", () => {
    expect(escapeControl("a\x00b\x7fc\x1b")).toBe("a␀b␡c␛");
    const f = prepareFile({ path: "a.txt", before: "a\x00\n", after: "b\x7f\n" });
    const texts = diffRows([f]).rows.filter((r) => r.type === "line").map((r) => r.text);
    expect(texts).toEqual(["a␀", "b␡"]);
    expect(texts.join("")).not.toMatch(/[\x00-\x1f\x7f]/);
  });

  test("a file name row per file in a multi-file diff (CHAT-111)", () => {
    const a = prepareFile({ path: "a.go", before: "1\n", after: "2\n" });
    const b = prepareFile({ path: "b.go", before: "", after: "new\n" });
    const rows = diffRows([a, b]).rows;
    expect(rows.filter((r) => r.type === "file").map((r) => r.path)).toEqual(["a.go", "b.go"]);
    expect(rows[0]).toEqual({ type: "file", path: "a.go" });
    expect(diffRows([a]).rows.some((r) => r.type === "file")).toBe(false);
  });

  test("the cap: exactly at the cap shows no '…'; one more line does (CHAT-112)", () => {
    const f = prepareFile({ path: "a.txt", before: "a\nb\nc\n", after: "x\ny\nz\n" });
    const { rows, total } = diffRows([f], { height: 0 });
    expect(total).toBe(7);
    expect(diffRows([f], { height: 7 }).rows.some((r) => r.type === "more")).toBe(false);
    const capped = diffRows([f], { height: 6 }).rows;
    expect(capped.length).toBe(6);
    expect(capped[5].type).toBe("more");
    expect(rows.length).toBe(7);
  });

  test("line numbers: unified numbers both sides, split one per side", () => {
    const f = prepareFile({ path: "a.txt", before: "a\nb\n", after: "a\nc\n" });
    expect(diffRows([f]).rows.filter((r) => r.type === "line").map((r) => [r.kind, r.before, r.after])).toEqual([
      [EQUAL, 1, 1],
      [DELETE, 2, null],
      [INSERT, null, 2],
    ]);
    const pair = diffRows([f], { layout: "split" }).rows.filter((r) => r.type === "pair")[1];
    expect([pair.left.number, pair.right.number, pair.left.kind, pair.right.kind]).toEqual([2, 2, DELETE, INSERT]);
  });

  test("the tint follows the paper's lightness; unknown is dark (DLG-310)", () => {
    expect(tintFor("#f9f8ef")).toBe("light");
    expect(tintFor("rgb(249, 248, 239)")).toBe("light");
    expect(tintFor("#14171d")).toBe("dark");
    expect(tintFor("#808080")).toBe("light");
    expect(tintFor("#7f7f7f")).toBe("dark");
    expect(tintFor("")).toBe("dark");
    expect(tintFor("var(--x)")).toBe("dark");
  });
});

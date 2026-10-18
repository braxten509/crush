// CHAT-045: program colors. StripCursorControl's cases are those of
// internal/ui/common/ansi16_test.go:99-200; RemapANSI16's cases (:19-97)
// become the class each color gets here.
import { describe, expect, test } from "vitest";
import { ansiLines, ansiToNodes, renderAnsi, rgb256, stripAnsi, stripCursorControl } from "./ansi.js";

// The spans of a fragment as [text, className, style color, style background].
function runs(fragment) {
  const out = [];
  for (const node of fragment.childNodes) {
    if (node.nodeType === 3) out.push([node.data, "", "", ""]);
    else out.push([node.textContent, node.className, node.style.color, node.style.backgroundColor]);
  }
  return out;
}

describe("StripCursorControl (ansi16_test.go)", () => {
  const cases = [
    ["plain text passes through", "hello world", "hello world"],
    ["SGR color sequences preserved", "\x1b[31mred\x1b[0m", "\x1b[31mred\x1b[0m"],
    ["cursor up stripped", "line1\n\x1b[Aline2", "line1\nline2"],
    ["cursor down stripped", "top\x1b[Bbottom", "topbottom"],
    ["erase line stripped", "progress\x1b[Kdone", "progressdone"],
    ["erase display stripped", "\x1b[2Jcleared", "cleared"],
    ["cursor position stripped", "\x1b[Hhome", "home"],
    ["DEC private mode hide cursor stripped", "\x1b[?25linvisible\x1b[?25hvisible", "invisiblevisible"],
    ["DEC save restore cursor stripped", "\x1b7saved\x1b8restored", "savedrestored"],
    ["save restore cursor CSI stripped", "\x1b[spos\x1b[upos", "pospos"],
    ["scroll up down stripped", "\x1b[Sscroll\x1b[Tscroll", "scrollscroll"],
    ["carriage return simulates overwrite", "loading...\rdone!", "done!"],
    ["multiple carriage returns keep last", "step1\rstep2\rstep3", "step3"],
    ["carriage return per line", "a\rb\nc\rd", "b\nd"],
    ["mixed SGR and cursor control", "\x1b[31mloading\x1b[0m\r\x1b[32mdone\x1b[0m", "\x1b[32mdone\x1b[0m"],
    [
      "git push style progress",
      "Enumerating objects: 10\rEnumerating objects: 50\rEnumerating objects: 100, done.",
      "Enumerating objects: 100, done.",
    ],
    ["no escapes fast path", "just text no escapes", "just text no escapes"],
  ];
  for (const [name, input, want] of cases) {
    test(name, () => expect(stripCursorControl(input)).toBe(want));
  }
});

describe("the 16 colors become classes (RemapANSI16's cases)", () => {
  test("no escapes passes through", () => {
    expect(runs(renderAnsi("plain text"))).toEqual([["plain text", "", "", ""]]);
  });
  test("standard and bright foreground", () => {
    expect(runs(renderAnsi("\x1b[31mhi\x1b[0m"))).toEqual([["hi", "ansi-fg-1", "", ""]]);
    expect(runs(renderAnsi("\x1b[91mhi\x1b[0m"))).toEqual([["hi", "ansi-fg-9", "", ""]]);
  });
  test("standard and bright background", () => {
    expect(runs(renderAnsi("\x1b[42mx\x1b[0m"))).toEqual([["x", "ansi-bg-2", "", ""]]);
    expect(runs(renderAnsi("\x1b[103mx\x1b[0m"))).toEqual([["x", "ansi-bg-11", "", ""]]);
  });
  test("bold plus color keeps both", () => {
    expect(runs(renderAnsi("\x1b[1;31mx\x1b[0m"))).toEqual([["x", "ansi-fg-1 ansi-bold", "", ""]]);
  });
  test("256 colors: 0-15 are the theme's, the rest set on the element", () => {
    expect(runs(renderAnsi("\x1b[38;5;4mx"))).toEqual([["x", "ansi-fg-4", "", ""]]);
    expect(runs(renderAnsi("\x1b[38;5;196mx\x1b[0m"))).toEqual([["x", "", "rgb(255, 0, 0)", ""]]);
    expect(runs(renderAnsi("\x1b[48;5;244mx"))).toEqual([["x", "", "", "rgb(128, 128, 128)"]]);
    expect(rgb256(16)).toEqual([0, 0, 0]);
    expect(rgb256(21)).toEqual([0, 0, 255]);
    expect(rgb256(231)).toEqual([255, 255, 255]);
    expect(rgb256(232)).toEqual([8, 8, 8]);
    expect(rgb256(255)).toEqual([238, 238, 238]);
  });
  test("true color, with semicolons and with colons", () => {
    expect(runs(renderAnsi("\x1b[38;2;10;20;30mx\x1b[0m"))).toEqual([["x", "", "rgb(10, 20, 30)", ""]]);
    expect(runs(renderAnsi("\x1b[38:2::10:20:30mx"))).toEqual([["x", "", "rgb(10, 20, 30)", ""]]);
    expect(runs(renderAnsi("\x1b[48;2;1;2;3mx"))).toEqual([["x", "", "", "rgb(1, 2, 3)"]]);
  });
  test("non-SGR CSI is dropped, the color stays", () => {
    expect(runs(renderAnsi("\x1b[2J\x1b[31mx"))).toEqual([["x", "ansi-fg-1", "", ""]]);
  });
  test("reset and default foreground leave plain text", () => {
    expect(runs(renderAnsi("\x1b[0;39mx"))).toEqual([["x", "", "", ""]]);
    expect(runs(renderAnsi("\x1b[31ma\x1b[39mb"))).toEqual([
      ["a", "ansi-fg-1", "", ""],
      ["b", "", "", ""],
    ]);
  });
  test("underline colors (58, 59) draw nothing of their own", () => {
    expect(runs(renderAnsi("\x1b[58;5;196mx\x1b[0m"))).toEqual([["x", "", "", ""]]);
    expect(runs(renderAnsi("\x1b[58;2;10;20;30mx\x1b[0m"))).toEqual([["x", "", "", ""]]);
    expect(runs(renderAnsi("\x1b[59mx"))).toEqual([["x", "", "", ""]]);
    // ... and do not eat the parameters that follow them.
    expect(runs(renderAnsi("\x1b[58;5;196;31mx"))).toEqual([["x", "ansi-fg-1", "", ""]]);
  });
  test("attributes and reverse video", () => {
    expect(runs(renderAnsi("\x1b[2;3;4;9mx"))).toEqual([["x", "ansi-dim ansi-italic ansi-underline ansi-strike", "", ""]]);
    expect(runs(renderAnsi("\x1b[7mx"))).toEqual([["x", "ansi-inverse ansi-fg-paper ansi-bg-ink", "", ""]]);
    expect(runs(renderAnsi("\x1b[7;31mx"))).toEqual([["x", "ansi-inverse ansi-fg-paper ansi-bg-1", "", ""]]);
    expect(runs(renderAnsi("\x1b[1;22mx"))).toEqual([["x", "", "", ""]]);
  });
});

describe("program output", () => {
  test("a trailing reset makes no empty span", () => {
    const fragment = renderAnsi("\x1b[32mok\x1b[0m");
    expect(runs(fragment)).toEqual([["ok", "ansi-fg-2", "", ""]]);
    expect(fragment.childNodes.length).toBe(1);
    expect(runs(renderAnsi("\x1b[0m"))).toEqual([]);
  });
  test("an unfinished color sequence at the end is dropped", () => {
    expect(runs(renderAnsi("done\x1b[3"))).toEqual([["done", "", "", ""]]);
  });
  test("a git progress bar collapses to its last state, colored", () => {
    const input =
      "remote: Counting objects:  50% (5/10)\rremote: Counting objects: 100% (10/10), done.\n" +
      "\x1b[32mTo github.com:o/r.git\x1b[0m\n";
    const fragment = renderAnsi(input);
    expect(fragment.textContent).toBe("remote: Counting objects: 100% (10/10), done.\nTo github.com:o/r.git\n");
    expect(runs(fragment)[1]).toEqual(["To github.com:o/r.git", "ansi-fg-2", "", ""]);
  });
  test("an npm progress bar (cursor hidden, line erased, redrawn) collapses", () => {
    const input =
      "\x1b[?25l[..................] / idealTree:app: sill idealTree\r\x1b[K" +
      "[######............] \\ reify:lodash: timing\r\x1b[K\x1b[?25h\n" +
      "added 1 package in 2s\n";
    expect(renderAnsi(input).textContent).toBe("\nadded 1 package in 2s\n");
  });
  test("text nodes only: no element but span, no attribute but class and style", () => {
    const fragment = renderAnsi("\x1b[1;38;2;1;2;3;48;5;100m<b onclick=x>\x1b[0m");
    const span = fragment.firstChild;
    expect(span.tagName).toBe("SPAN");
    expect(span.textContent).toBe("<b onclick=x>");
    expect(span.children.length).toBe(0);
    expect([...span.attributes].map((a) => a.name).sort()).toEqual(["class", "style"]);
  });
  test("lines carry their colors to the next line", () => {
    const lines = ansiLines("\x1b[31ma\nb\x1b[0m\nc");
    expect(lines.map((line) => runs(line))).toEqual([[["a", "ansi-fg-1", "", ""]], [["b", "ansi-fg-1", "", ""]], [["c", "", "", ""]]]);
  });
  test("ansiToNodes keeps cursor-control text as is but drops the sequences", () => {
    expect(ansiToNodes("a\x1b[2Kb").textContent).toBe("ab");
  });
  test("stripAnsi (charmbracelet/x/ansi Strip)", () => {
    expect(stripAnsi("\x1b[31mred\x1b[0m")).toBe("red");
    expect(stripAnsi("a\x1b]8;;https://x\x07link\x1b]8;;\x07b")).toBe("alinkb");
    expect(stripAnsi("a\x1bPq#0\x1b\\b")).toBe("ab");
    expect(stripAnsi("tab\there\r\nDEL\x7f")).toBe("tab\there\r\nDEL");
  });
});

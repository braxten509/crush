// CHAT-018: incremental Markdown with a stable prefix. At every step of a
// stream the page must equal a full render of the same text; a retry that
// rewrites earlier text drops the cache; the boundary never moves back.
import { describe, expect, test } from "vitest";
import { RELAX_AFTER, stableBoundary } from "./boundary.js";
import { createMarkdown } from "./incremental.js";
import { lexMarkdown, prepare } from "./lex.js";
import { renderMarkdown } from "./markdown.js";

const serialize = (node) => new XMLSerializer().serializeToString(node);

function freshRoot(text, finished) {
  return renderMarkdown(text, { messageId: "m", sessionId: "s", finished });
}

// Streams text in chunks and checks every step against a full render, and
// that the boundary never moves back (only a link definition may move it).
function stream(text, sizes = [1, 3, 7, 2, 11, 5], { monotonic = true } = {}) {
  const root = document.createElement("div");
  root.className = "md";
  const renderer = createMarkdown(root, { messageId: "m", sessionId: "s" });
  let at = 0;
  let step = 0;
  let last = 0;
  while (at < text.length) {
    at = Math.min(text.length, at + sizes[step++ % sizes.length]);
    const prefix = text.slice(0, at);
    renderer.update(prefix, { finished: false });
    expect(serialize(root), `after ${at} of ${text.length}: ${JSON.stringify(prefix.slice(-30))}`).toBe(serialize(freshRoot(prefix, false)));
    const { text: lexed } = prepare(prefix);
    const boundary = stableBoundary(lexed, { lex: lexMarkdown });
    if (monotonic) expect(boundary, `boundary moved back at ${at}`).toBeGreaterThanOrEqual(last);
    last = boundary;
  }
  renderer.update(text, { finished: true });
  expect(serialize(root)).toBe(serialize(freshRoot(text, true)));
  return { root, renderer };
}

const DOC = [
  "# Release notes",
  "",
  "Intro paragraph with **bold**, `code`, a [link](https://example.com) and $x^2$.",
  "A soft-wrapped second line.",
  "",
  "- first item",
  "- second item with `code`",
  "  - nested item",
  "",
  "After the list.",
  "",
  "1. one",
  "2. two",
  "",
  "   continued",
  "",
  "> a quote",
  "> over two lines",
  "",
  "```go",
  "func main() {",
  "",
  "\tfmt.Println(\"hi\")",
  "}",
  "```",
  "",
  "| a | b |",
  "|---|---|",
  "| 1 | 2 |",
  "",
  "$$",
  "\\frac{a}{b}",
  "$$",
  "",
  "```mermaid",
  "flowchart LR",
  "  A[One] --> B[Two]",
  "```",
  "",
  "Setext heading",
  "---",
  "",
  "---",
  "",
  "<div>raw <b>html</b></div>",
  "",
  "- [ ] task",
  "- [x] done",
  "",
  "Last paragraph.",
].join("\n");

describe("the stream equals a full render at every step", () => {
  test("a document with every block type", () => {
    stream(DOC);
  });

  test("a closed then reopened fence", () => {
    const text = "```\na\n\nb\n```\n\ntext between\n\n```js\nc\n\nd\n\ne";
    stream(text, [2, 1, 4]);
    // No boundary ever lands inside the reopened fence.
    const reopen = text.indexOf("```js");
    for (let at = reopen + 6; at <= text.length; at++) {
      expect(stableBoundary(text.slice(0, at), { lex: lexMarkdown })).toBeLessThanOrEqual(reopen);
    }
  });

  test("a fence the terminal's parity count thinks is closed", () => {
    // "````" opens; "```" cannot close it, so the block is still open.
    stream("````md\n```\n\ninside\n\n```\n````\n\nafter\n\nmore", [1, 2]);
  });

  test("a 2 KB reply with no blank line stays one paragraph", () => {
    const line = "words that wrap across the reading column without a break ";
    let text = "";
    while (text.length < 2 * RELAX_AFTER) text += `${line}${text.length}\n`;
    stream(text, [97, 13, 51]);
    // No cut can land inside the paragraph, however long it is.
    expect(stableBoundary(text, { lex: lexMarkdown })).toBe(0);
  });

  test("a long reply without blank lines but with block edges takes the relaxed cut", () => {
    let text = "";
    for (let i = 0; text.length < 2 * RELAX_AFTER; i++) text += `## Part ${i}\nSome words for part ${i}, long enough to fill a line of text.\n`;
    stream(text, [89, 17, 41]);
    // The cut lands between two blocks, near the end: the tail is one block.
    const boundary = stableBoundary(text, { lex: lexMarkdown });
    expect(boundary).toBeGreaterThan(text.length - 200);
    expect(text[boundary - 1]).toBe("\n");
    expect(text.slice(boundary)).toMatch(/^(## Part|Some words)/);
  });

  test("a link reference definition anywhere means nothing is stable", () => {
    const text = "See [the docs][1].\n\nMore text.\n\n[1]: https://example.com\n";
    stream(text, [3], { monotonic: false });
    expect(stableBoundary(text, { lex: lexMarkdown })).toBe(0);
  });

  test("plan markers and \\r\\n stream like any text", () => {
    stream("<!-- CRUSH_PLAN_START -->\r\n# Plan\r\n\r\n- one\r\n- two\r\n\r\nDone.\r\n<!-- CRUSH_PLAN_READY -->", [4, 9]);
  });
});

describe("the cache", () => {
  test("stable blocks keep their nodes while the reply grows", () => {
    const root = document.createElement("div");
    const renderer = createMarkdown(root, { messageId: "m" });
    renderer.update("First paragraph.\n\nSecond", { finished: false });
    const first = root.firstChild;
    renderer.update("First paragraph.\n\nSecond paragraph grows", { finished: false });
    expect(root.firstChild).toBe(first);
    renderer.update("First paragraph.\n\nSecond paragraph grows.\n\nThird", { finished: false });
    expect(root.firstChild).toBe(first);
    expect(root.children.length).toBe(3);
  });

  test("a retry that rewrites earlier text drops the cache", () => {
    const root = document.createElement("div");
    root.className = "md";
    const renderer = createMarkdown(root, { messageId: "m" });
    renderer.update("Alpha paragraph.\n\nBeta paragraph.\n\nGamma", { finished: false });
    const first = root.firstChild;
    renderer.update("Rewritten start.\n\nBeta", { finished: false });
    expect(root.firstChild).not.toBe(first);
    expect(serialize(root)).toBe(serialize(freshRoot("Rewritten start.\n\nBeta", false)));
  });

  test("finishing turns the tail's chips into figures", () => {
    const root = document.createElement("div");
    const renderer = createMarkdown(root, { messageId: "m" });
    renderer.update("Intro\n\n| a |\n|---|\n| 1 |", { finished: false });
    expect(root.querySelector(".md-chip").dataset.figure).toBeUndefined();
    renderer.update("Intro\n\n| a |\n|---|\n| 1 |", { finished: true });
    expect(root.querySelector(".md-chip").dataset.figure).toBe("m:0");
  });

  test("released visuals: every chip that leaves the page is handed back once", () => {
    const root = document.createElement("div");
    const made = new Set();
    const released = [];
    const renderer = createMarkdown(root, {
      messageId: "m",
      visual() {
        const node = document.createElement("span");
        made.add(node);
        return node;
      },
      release: (node) => released.push(node),
    });
    renderer.update("$$\nx\n$$\n\ntail $$y$$", { finished: false });
    renderer.update("$$\nx\n$$\n\ntail $$y$$ more\n\n$$\nz\n$$", { finished: false });
    renderer.update("rewritten", { finished: false });
    renderer.destroy();
    expect(new Set(released).size).toBe(released.length);
    for (const node of released) expect(made.has(node)).toBe(true);
    expect(released.length).toBe(made.size);
    expect(root.childNodes.length).toBe(0);
  });

  test("decorations redraw, split around their text", () => {
    const root = document.createElement("div");
    const renderer = createMarkdown(root, { messageId: "m" });
    const text = "one two three";
    renderer.update(text, { finished: true });
    expect(root.querySelector(".md-deco")).toBeNull();
    renderer.update(text, { finished: true, decorations: [{ kind: "branch", start: 4, end: 7 }] });
    expect(root.querySelector(".md-deco").textContent).toBe("two");
    expect(root.textContent).toBe(text);
  });
});

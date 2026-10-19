// CHAT-088's base: the renderer's source map. Every shown character points at
// the stored character it came from; a page selection turns back into a range
// of the stored Markdown, whole inline spans bringing their marks.
import { describe, expect, test } from "vitest";
import { renderMarkdown } from "./markdown.js";
import { PLAN_START_MARKER } from "./plan.js";
import { sourceMapOf } from "./sourcemap.js";

function textNodes(root) {
  const out = [];
  const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT);
  for (let node = walker.nextNode(); node; node = walker.nextNode()) out.push(node);
  return out;
}

// Every registered character of a one-to-one run equals its stored character.
function expectFaithful(root, stored) {
  const map = sourceMapOf(root);
  let checked = 0;
  for (const node of textNodes(root)) {
    const runs = map.texts.get(node);
    if (!runs) continue;
    for (let i = 0; i < runs.length; i += 4) {
      const [d0, d1, s0, s1] = runs.slice(i, i + 4);
      expect(s1).toBeGreaterThanOrEqual(s0);
      if (d1 - d0 !== s1 - s0) continue;
      const shown = node.data.slice(d0, d1);
      const source = stored.slice(s0, s1);
      // Inline code shows a newline as a space; everything else is identical.
      expect(shown, `node ${JSON.stringify(node.data)}`).toBe(node.parentElement?.closest("code.md-icode") ? source.replace(/\n/g, " ") : source);
      checked += d1 - d0;
    }
  }
  return checked;
}

// The range over the first occurrence of `word` in the rendered text nodes.
function rangeOf(root, word, nth = 0) {
  for (const node of textNodes(root)) {
    let at = -1;
    let seen = 0;
    while ((at = node.data.indexOf(word, at + 1)) >= 0) {
      if (seen++ === nth) {
        const range = document.createRange();
        range.setStart(node, at);
        range.setEnd(node, at + word.length);
        return range;
      }
    }
  }
  throw new Error(`no ${word}`);
}

// A range over the whole text content of an element.
function rangeOver(el) {
  const range = document.createRange();
  range.selectNodeContents(el);
  return range;
}

const SAMPLES = [
  "Plain words and **bold** and *em* and `code` and [a link](https://example.com).",
  "# Heading\n\nA paragraph\nwith a soft break.\n\n- one\n- two\n  - nested `x`\n\n1. first\n2. second\n",
  "> quote with **marks**\n> second line\n\nafter",
  "```go\nfunc main() {\n\tx := 1\n}\n```\n\ntext &amp; &copy; \\* escaped",
  "- [ ] task\n- [x] done\n\n| a | b |\n|---|---|\n| 1 | 2 |\n\n$$\nx^2\n$$\n",
  "Windows\r\nline ends\r\n\r\nand a **second** paragraph\r\n",
  `${PLAN_START_MARKER}\n# Plan\n\n- step one\n- step two`,
  "- item\n\n  continued paragraph\n\n  > nested quote\n\n  ```\n  code in item\n  ```\n",
];

describe("every shown character points at its stored character", () => {
  for (const stored of SAMPLES) {
    test(JSON.stringify(stored.slice(0, 40)), () => {
      const root = renderMarkdown(stored);
      expect(expectFaithful(root, stored)).toBeGreaterThan(0);
    });
  }
});

describe("rangeToSource", () => {
  test("a word", () => {
    const stored = "Plain words and **bold** here.";
    const root = renderMarkdown(stored);
    const cut = sourceMapOf(root).rangeToSource(rangeOf(root, "words"));
    expect(stored.slice(cut.start, cut.end)).toBe("words");
  });

  test("whole inline spans bring their marks; part of one does not", () => {
    const stored = "a **bold** b `code` c [label](https://x.example) d ~~gone~~";
    const root = renderMarkdown(stored);
    const map = sourceMapOf(root);
    const cut = (range) => {
      const r = map.rangeToSource(range);
      return stored.slice(r.start, r.end);
    };
    expect(cut(rangeOver(root.querySelector("strong")))).toBe("**bold**");
    expect(cut(rangeOf(root, "bol"))).toBe("bol");
    expect(cut(rangeOver(root.querySelector("code")))).toBe("`code`");
    expect(cut(rangeOf(root, "cod"))).toBe("cod");
    expect(cut(rangeOver(root.querySelector("a")))).toBe("[label](https://x.example)");
    expect(cut(rangeOver(root.querySelector("del")))).toBe("~~gone~~");
    expect(cut(rangeOver(root.firstChild))).toBe(stored);
  });

  test("nested marks come back whole", () => {
    const stored = "x **bold `code` bold** y";
    const root = renderMarkdown(stored);
    const r = sourceMapOf(root).rangeToSource(rangeOver(root.querySelector("code")));
    expect(stored.slice(r.start, r.end)).toBe("`code`");
    const whole = sourceMapOf(root).rangeToSource(rangeOver(root.querySelector("strong")));
    expect(stored.slice(whole.start, whole.end)).toBe("**bold `code` bold**");
  });

  test("across blocks the blank lines come along", () => {
    const stored = "first para\n\nsecond para";
    const root = renderMarkdown(stored);
    const range = document.createRange();
    const a = root.children[0].firstChild;
    const b = root.children[1].firstChild;
    range.setStart(a, 6);
    range.setEnd(b, 6);
    const r = sourceMapOf(root).rangeToSource(range);
    expect(stored.slice(r.start, r.end)).toBe("para\n\nsecond");
  });

  test("a code block: its text, or the whole block with its fences", () => {
    const stored = "before\n\n```js\nlet a = 1;\n\tb();\n```\n\nafter";
    const root = renderMarkdown(stored);
    const map = sourceMapOf(root);
    const body = root.querySelector(".md-code-body");
    let r = map.rangeToSource(rangeOver(body));
    expect(stored.slice(r.start, r.end)).toBe("let a = 1;\n\tb();");
    const range = document.createRange();
    range.setStart(root, 1);
    range.setEnd(root, 2);
    r = map.rangeToSource(range);
    expect(stored.slice(r.start, r.end)).toBe("```js\nlet a = 1;\n\tb();\n```");
  });

  test("entities and escapes give back what was written", () => {
    const stored = "Tom &amp; Jerry \\* &copy;";
    const root = renderMarkdown(stored);
    const map = sourceMapOf(root);
    const cut = (word) => {
      const r = map.rangeToSource(rangeOf(root, word));
      return stored.slice(r.start, r.end);
    };
    expect(cut("&")).toBe("&amp;");
    expect(cut("*")).toBe("\\*");
    expect(cut("©")).toBe("&copy;");
    expect(cut("Jerry")).toBe("Jerry");
  });

  test("raw HTML: its words point past the tags; an entity keeps its whole reference", () => {
    const cut = (stored, word, nth = 0) => {
      const root = renderMarkdown(stored);
      const r = sourceMapOf(root).rangeToSource(rangeOf(root, word, nth));
      return stored.slice(r.start, r.end);
    };
    // Plain HTML whose tag name shares the word's first letters.
    expect(cut("<div>dividend</div>", "dividend")).toBe("dividend");
    expect(cut("Before.\n\n<div>dividend</div>", "divi")).toBe("divi");
    // Nested tags: each word is its own characters; across a tag the tag comes along.
    const nested = "<section>\n<div><p>first <b>bold</b> last</p></div>\n</section>";
    expect(cut(nested, "first")).toBe("first");
    expect(cut(nested, "bold")).toBe("bold");
    expect(cut(nested, "last")).toBe("last");
    expect(cut(nested, "first bold last")).toBe("first <b>bold</b> last");
    // An entity: the decoded character stands for the whole reference.
    expect(cut("<div>Tom &amp; Jerry &copy; 2026</div>", "&")).toBe("&amp;");
    expect(cut("<div>Tom &amp; Jerry &copy; 2026</div>", "©")).toBe("&copy;");
    expect(cut("<div>Tom &amp; Jerry &copy; 2026</div>", "Jerry © 2026")).toBe("Jerry &copy; 2026");
    // A ">" inside a quoted attribute, a comment and a style element's content are not text.
    const tricky = '<div title="a>word">word<!-- word --><style>p{content:"word"}</style> tail &amp; end</div>';
    expect(cut(tricky, "word")).toBe("word");
    expect(cut(tricky, "tail & end")).toBe("tail &amp; end");
  });

  test("raw HTML: comments close where the HTML parser closes them", () => {
    const range = (stored, word) => {
      const root = renderMarkdown(stored);
      expect(root.textContent).toContain(word);
      return sourceMapOf(root).rangeToSource(rangeOf(root, word));
    };
    // "<!-->" and "<!--->" are whole (empty) comments; "--!>" closes one too.
    for (const stored of ["<div><!-->hello</div>", "<div><!--->hello</div>", "<div><!-- a --!>hello</div>", "<div><!---->hello</div>"]) {
      const r = range(stored, "hello");
      expect([r?.start, r?.end], stored).toEqual([stored.indexOf("hello"), stored.indexOf("hello") + 5]);
    }
  });

  test("raw HTML: a letter that grows when lowercased does not shift the positions", () => {
    // "İ" lowercases to two characters; the visible "visible" is the second one.
    const stored = "<div>İ<script>visible</script>visible</div>";
    const root = renderMarkdown(stored);
    const r = sourceMapOf(root).rangeToSource(rangeOf(root, "visible"));
    expect([r.start, r.end]).toEqual([30, 37]);
    expect(stored.slice(r.start, r.end)).toBe("visible");
  });

  test("raw HTML: a hidden element ends only at its own end tag, in any case", () => {
    // "</scripts>" is still script text; "</Script>" ends it.
    const stored = "<div><SCRIPT>a</scripts>visible</Script>visible</div>";
    const root = renderMarkdown(stored);
    expect(root.textContent.trim()).toBe("visible");
    const r = sourceMapOf(root).rangeToSource(rangeOf(root, "visible"));
    expect([r?.start, r?.end]).toEqual([40, 47]);
  });

  test('raw HTML: a "</" at the very end is text', () => {
    // The HTML parser shows a "</" that the input ends on as the two characters.
    const stored = "<div>a</";
    const root = renderMarkdown(stored);
    expect(root.textContent).toContain("a</");
    const r = sourceMapOf(root).rangeToSource(rangeOf(root, "a</"));
    expect([r?.start, r?.end]).toEqual([5, 8]);
  });

  test("atoms: a task box, inline math and a chip come whole", () => {
    const stored = "- [x] done $a^2$ now\n\n| t | u |\n|---|---|\n| 1 | 2 |";
    const root = renderMarkdown(stored);
    const map = sourceMapOf(root);
    const span = (el) => {
      const range = document.createRange();
      range.selectNode(el);
      const r = map.rangeToSource(range);
      return stored.slice(r.start, r.end);
    };
    expect(span(root.querySelector(".md-taskbox"))).toBe("[x] ");
    expect(span(root.querySelector(".md-math"))).toBe("$a^2$");
    expect(span(root.querySelector(".md-chip"))).toBe("| t | u |\n|---|---|\n| 1 | 2 |");
  });

  test("inside quotes and list items the prefixes stay out", () => {
    const stored = "> quoted words\n> more\n\n- item words\n\n  continued words\n";
    const root = renderMarkdown(stored);
    const map = sourceMapOf(root);
    for (const [word, nth] of [["quoted", 0], ["more", 0], ["item", 0], ["continued", 0]]) {
      const r = map.rangeToSource(rangeOf(root, word, nth));
      expect(stored.slice(r.start, r.end)).toBe(word);
    }
    const range = document.createRange();
    range.setStart(rangeOf(root, "words", 0).startContainer, rangeOf(root, "words", 0).startOffset);
    range.setEnd(rangeOf(root, "more").endContainer, rangeOf(root, "more").endOffset);
    const r = map.rangeToSource(range);
    expect(stored.slice(r.start, r.end)).toBe("words\n> more");
  });

  test("stored \\r\\n and a stripped plan marker keep stored offsets", () => {
    const stored = `${PLAN_START_MARKER}\r\n# Plan\r\n\r\nstep **one**\r\n`;
    const root = renderMarkdown(stored);
    const map = sourceMapOf(root);
    let r = map.rangeToSource(rangeOf(root, "step"));
    expect(stored.slice(r.start, r.end)).toBe("step");
    r = map.rangeToSource(rangeOver(root.querySelector("strong")));
    expect(stored.slice(r.start, r.end)).toBe("**one**");
  });

  test("a collapsed or empty range gives null", () => {
    const root = renderMarkdown("words");
    const range = document.createRange();
    range.setStart(root.firstChild.firstChild, 2);
    range.collapse(true);
    expect(sourceMapOf(root).rangeToSource(range)).toBeNull();
  });
});

describe("pointToSource and sourceToRange", () => {
  test("a point between characters, both sides", () => {
    const stored = "ab **cd** ef";
    const root = renderMarkdown(stored);
    const map = sourceMapOf(root);
    const strong = root.querySelector("strong");
    expect(map.pointToSource(strong.firstChild, 0, "start")).toBe(stored.indexOf("cd"));
    expect(map.pointToSource(strong.firstChild, 2, "end")).toBe(stored.indexOf("cd") + 2);
    // Points outside the root clamp to its ends: before it, after it, or in
    // an element around it on either side.
    const before = document.createElement("p");
    const after = document.createElement("p");
    document.body.append(before, root, after);
    expect(map.pointToSource(before, 0, "start")).toBe(0);
    expect(map.pointToSource(after, 0, "end")).toBe(stored.length);
    const at = [...document.body.childNodes].indexOf(root);
    expect(map.pointToSource(document.body, at, "start")).toBe(0);
    expect(map.pointToSource(document.body, at + 1, "end")).toBe(stored.length);
    before.remove();
    root.remove();
    after.remove();
  });

  test("a stored range back to the page", () => {
    const stored = "one **two** three\n\nfour";
    const root = renderMarkdown(stored);
    document.body.append(root);
    const map = sourceMapOf(root);
    const range = map.sourceToRange(stored.indexOf("two"), stored.indexOf("three") + 5);
    expect(range.toString()).toBe("two three");
    const across = map.sourceToRange(stored.indexOf("three"), stored.length);
    expect(across.toString()).toBe("threefour");
    root.remove();
  });
});
